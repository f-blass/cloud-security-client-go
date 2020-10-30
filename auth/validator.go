// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Cloud Security Client Go contributors
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"github.com/dgrijalva/jwt-go/v4"
	"github.com/sap-staging/cloud-security-client-go/oidcclient"
	"net/url"
	"strings"
	"time"
)

func (m *AuthMiddleware) ParseAndValidateJWT(rawToken string) (*jwt.Token, error) {
	token, parts, err := m.parser.ParseUnverified(rawToken, new(OIDCClaims))
	if err != nil {
		return nil, err
	}
	token.Signature = parts[2]

	// get keyset
	keySet, err := m.getOIDCTenant(token)
	if err != nil {
		return nil, err
	}

	// verify claims
	if err := m.validateClaims(token, keySet); err != nil {
		return nil, err
	}

	// verify signature
	if err = m.verifySignature(token, keySet); err != nil {
		return nil, err
	}

	mapClaims, _, err := m.parser.ParseUnverified(rawToken, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}
	token.Claims.(*OIDCClaims).mapClaims = mapClaims.Claims.(jwt.MapClaims)

	token.Valid = true
	return token, nil
}

func (m *AuthMiddleware) verifySignature(t *jwt.Token, ks *oidcclient.OIDCTenant) error {
	jwks, err := ks.GetJWKs()
	if err != nil {
		return wrapError(&jwt.UnverfiableTokenError{Message: "failed to fetch token keys from remote"}, err)
	}
	if len(jwks) == 0 {
		return &jwt.UnverfiableTokenError{Message: "remote returned no jwk to verify the token"}
	}

	var jwk *oidcclient.JSONWebKey

	if kid := t.Header[propKeyID]; kid != nil {
		for _, key := range jwks {
			if key.Kid == kid {
				jwk = key
				break
			}
		}
		if jwk == nil {
			return &jwt.UnverfiableTokenError{Message: "kid id specified in token not presented by remote"}
		}
	} else if len(jwks) == 1 {
		jwk = jwks[0]
	} else {
		return &jwt.UnverfiableTokenError{Message: "no kid specified in token and more than one verification key available"}
	}

	// join token together again, as t.Raw does not contain signature
	if err := t.Method.Verify(strings.TrimSuffix(t.Raw, "."+t.Signature), t.Signature, jwk.Key); err != nil {
		// invalid
		return wrapError(&jwt.InvalidSignatureError{}, err)
	}
	return nil
}

func (m *AuthMiddleware) validateClaims(t *jwt.Token, ks *oidcclient.OIDCTenant) error {
	c := t.Claims.(*OIDCClaims)

	if c.ExpiresAt == nil {
		return &jwt.UnverfiableTokenError{Message: "expiration time (exp) is unavailable."}
	}
	validationHelper := jwt.NewValidationHelper(
		jwt.WithAudience(m.options.OAuthConfig.GetClientID()),
		jwt.WithIssuer(ks.ProviderJSON.Issuer),
		jwt.WithLeeway(1*time.Minute),
	)

	err := c.Valid(validationHelper)

	return err
}

func (m *AuthMiddleware) getOIDCTenant(t *jwt.Token) (*oidcclient.OIDCTenant, error) {
	claims, ok := t.Claims.(*OIDCClaims)
	if !ok {
		return nil, &jwt.UnverfiableTokenError{
			Message: fmt.Sprintf("internal validation error during type assertion: expected *OIDCClaims, got %T", t.Claims)}
	}

	iss := claims.Issuer
	issURI, err := url.ParseRequestURI(iss)
	if err != nil {
		return nil, fmt.Errorf("unable to parse issuer URI: %s", iss)
	}

	bindingIssURI, err := url.ParseRequestURI(m.options.OAuthConfig.GetURL())
	if err != nil {
		return nil, fmt.Errorf("unable to parse issuer URI: %s", iss)
	}

	// TODO: replace this check later against domain property from binding to enable multi tenancy support
	if bindingIssURI.Hostname() != issURI.Hostname() {
		return nil, &jwt.UnverfiableTokenError{Message: "token is issued by unsupported oauth server"}
	}

	keySet, exp, found := m.oidcTenants.GetWithExpiration(iss)
	if !found || time.Now().After(exp) {
		newKeySet, err, _ := m.sf.Do(iss, func() (i interface{}, err error) {
			set, err := oidcclient.NewOIDCTenant(m.options.HTTPClient, issURI)
			return set, err
		})

		if err != nil {
			return nil, wrapError(&jwt.UnverfiableTokenError{Message: "unable to build remote keyset"}, err)
		}
		keySet = newKeySet.(*oidcclient.OIDCTenant)
		m.oidcTenants.SetDefault(keySet.(*oidcclient.OIDCTenant).ProviderJSON.Issuer, keySet)
	}
	return keySet.(*oidcclient.OIDCTenant), nil
}

func wrapError(a, b error) error {
	if b == nil {
		return a
	}
	if a == nil {
		return b
	}

	type iErrorWrapper interface {
		Wrap(error)
	}
	if w, ok := a.(iErrorWrapper); ok {
		w.Wrap(b)
	}
	return a
}
