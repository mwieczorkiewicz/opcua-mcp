package opcua

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uapolicy"
)

// secureChannelEnabled reports whether the configuration asks for a signed
// and/or encrypted secure channel.
func (c *Client) secureChannelEnabled() bool {
	return c.config.SecurityPolicy != "None" || c.config.SecurityMode != "None"
}

// addChannelSecurity appends the options needed for a Sign/SignAndEncrypt
// secure channel. It must run after the Auth* option so that
// SecurityFromEndpoint fills in the server's user-token PolicyID on the
// already-chosen identity token rather than replacing it.
//
// The client certificate/key (OPCUA_CERT_FILE/OPCUA_KEY_FILE) is used for the
// channel regardless of auth mode, so anonymous or username sessions over an
// encrypted channel work. The server certificate is taken from the matching
// endpoint returned by GetEndpoints; when OPCUA_SERVER_CERT is set, it pins
// that certificate and the connection is refused on mismatch.
func (c *Client) addChannelSecurity(ctx context.Context, opts []opcua.Option) ([]opcua.Option, error) {
	if !c.secureChannelEnabled() {
		return opts, nil
	}

	// certificate auth mode already added the client certificate and key
	if c.config.AuthMode != "certificate" {
		cert, key, err := loadClientKeyPair(c.config.CertFile, c.config.KeyFile)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opcua.Certificate(cert), opcua.PrivateKey(key))
	}

	endpoints, err := opcua.GetEndpoints(ctx, c.config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints: %w", err)
	}
	ep, err := opcua.SelectEndpoint(endpoints, c.config.SecurityPolicy, ua.MessageSecurityModeFromString(c.config.SecurityMode))
	if err != nil {
		return nil, fmt.Errorf("no endpoint matches security policy %q and mode %q: %w", c.config.SecurityPolicy, c.config.SecurityMode, err)
	}
	if ep == nil {
		return nil, fmt.Errorf("no endpoint matches security policy %q and mode %q", c.config.SecurityPolicy, c.config.SecurityMode)
	}

	if c.config.ServerCert != "" {
		if err := verifyServerCert(c.config.ServerCert, ep.ServerCertificate); err != nil {
			return nil, err
		}
	}

	return append(opts, opcua.SecurityFromEndpoint(ep, userTokenType(c.config.AuthMode))), nil
}

// loadClientKeyPair loads a PEM certificate/key pair and returns the DER
// certificate and RSA private key gopcua expects.
func loadClientKeyPair(certFile, keyFile string) ([]byte, *rsa.PrivateKey, error) {
	if certFile == "" || keyFile == "" {
		return nil, nil, fmt.Errorf("OPCUA_CERT_FILE and OPCUA_KEY_FILE are required when security policy/mode is not None")
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load client certificate: %w", err)
	}
	key, ok := pair.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("private key is not an RSA private key")
	}
	return pair.Certificate[0], key, nil
}

// verifyServerCert compares the endpoint's certificate with the pinned one.
// The pinned file may be DER or PEM.
func verifyServerCert(pinnedFile string, endpointCert []byte) error {
	data, err := os.ReadFile(pinnedFile)
	if err != nil {
		return fmt.Errorf("failed to read server certificate: %w", err)
	}
	pinned := data
	if block, _ := pem.Decode(data); block != nil {
		pinned = block.Bytes
	}
	if !bytes.Equal(uapolicy.Thumbprint(pinned), uapolicy.Thumbprint(endpointCert)) {
		return fmt.Errorf("server certificate does not match OPCUA_SERVER_CERT")
	}
	return nil
}

// userTokenType maps OPCUA_AUTH_MODE to the user identity token type.
func userTokenType(authMode string) ua.UserTokenType {
	switch authMode {
	case "username":
		return ua.UserTokenTypeUserName
	case "certificate":
		return ua.UserTokenTypeCertificate
	default:
		return ua.UserTokenTypeAnonymous
	}
}
