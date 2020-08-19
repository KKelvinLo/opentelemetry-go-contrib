// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cortex

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// createFile writes a file with a slice of bytes at a specified filepath.
func createFile(bytes []byte, filepath string) error {
	err := ioutil.WriteFile(filepath, bytes, 0644)
	if err != nil {
		return err
	}
	return nil
}

// TestAuthentication checks whether http requests are properly authenticated with either
// bearer tokens or basic authentication in the addHeaders method.
func TestAuthentication(t *testing.T) {
	tests := []struct {
		testName                      string
		basicAuth                     map[string]string
		basicAuthPasswordFileContents []byte
		bearerToken                   string
		bearerTokenFile               string
		bearerTokenFileContents       []byte
		expectedAuthHeaderValue       string
		expectedError                 error
	}{
		{
			testName: "Basic Auth with password",
			basicAuth: map[string]string{
				"username": "TestUser",
				"password": "TestPassword",
			},
			expectedAuthHeaderValue: "Basic " + base64.StdEncoding.EncodeToString(
				[]byte("TestUser:TestPassword"),
			),
			expectedError: nil,
		},
		{
			testName: "Basic Auth with no username",
			basicAuth: map[string]string{
				"password": "TestPassword",
			},
			expectedAuthHeaderValue: "",
			expectedError:           ErrNoBasicAuthUsername,
		},
		{
			testName: "Basic Auth with no password",
			basicAuth: map[string]string{
				"username": "TestUser",
			},
			expectedAuthHeaderValue: "",
			expectedError:           ErrNoBasicAuthPassword,
		},
		{
			testName: "Basic Auth with password file",
			basicAuth: map[string]string{
				"username":      "TestUser",
				"password_file": "passwordFile",
			},
			basicAuthPasswordFileContents: []byte("TestPassword"),
			expectedAuthHeaderValue: "Basic " + base64.StdEncoding.EncodeToString(
				[]byte("TestUser:TestPassword"),
			),
			expectedError: nil,
		},
		{
			testName: "Basic Auth with bad password file",
			basicAuth: map[string]string{
				"username":      "TestUser",
				"password_file": "missingPasswordFile",
			},
			expectedAuthHeaderValue: "",
			expectedError:           ErrFailedToReadFile,
		},
		{
			testName:                "Bearer Token",
			bearerToken:             "testToken",
			expectedAuthHeaderValue: "Bearer testToken",
			expectedError:           nil,
		},
		{
			testName:                "Bearer Token with bad bearer token file",
			bearerTokenFile:         "missingBearerTokenFile",
			expectedAuthHeaderValue: "",
			expectedError:           ErrFailedToReadFile,
		},
		{
			testName:                "Bearer Token with bearer token file",
			bearerTokenFile:         "bearerTokenFile",
			expectedAuthHeaderValue: "Bearer testToken",
			bearerTokenFileContents: []byte("testToken"),
			expectedError:           nil,
		},
	}
	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			// Set up a test server that runs a handler function when it receives a http
			// request. The server writes the request's Authorization header to the
			// response body.
			handler := func(rw http.ResponseWriter, req *http.Request) {
				authHeaderValue := req.Header.Get("Authorization")
				rw.Write([]byte(authHeaderValue))
			}
			server := httptest.NewServer(http.HandlerFunc(handler))
			defer server.Close()

			// Create the necessary files for tests.
			if test.basicAuth != nil {
				passwordFile := test.basicAuth["password_file"]
				if passwordFile != "" && test.basicAuthPasswordFileContents != nil {
					filepath := "./" + test.basicAuth["password_file"]
					err := createFile(test.basicAuthPasswordFileContents, filepath)
					require.Nil(t, err)
					defer os.Remove(filepath)
				}
			}
			if test.bearerTokenFile != "" && test.bearerTokenFileContents != nil {
				filepath := "./" + test.bearerTokenFile
				err := createFile(test.bearerTokenFileContents, filepath)
				require.Nil(t, err)
				defer os.Remove(filepath)
			}

			// Create a HTTP request and add headers to it through an Exporter. Since the
			// Exporter has an empty Headers map, authentication methods will be called.
			exporter := Exporter{
				Config{
					BasicAuth:       test.basicAuth,
					BearerToken:     test.bearerToken,
					BearerTokenFile: test.bearerTokenFile,
				},
			}
			req, err := http.NewRequest(http.MethodPost, server.URL, nil)
			require.Nil(t, err)
			err = exporter.addHeaders(req)

			// Verify the error and if the Authorization header was correctly set.
			if err != nil {
				require.Equal(t, err.Error(), test.expectedError.Error())
			} else {
				require.Nil(t, test.expectedError)
				authHeaderValue := req.Header.Get("Authorization")
				require.Equal(t, authHeaderValue, test.expectedAuthHeaderValue)
			}
		})
	}
}

// TestBuildClient checks whether the buildClient successfully creates a client that can
// connect over TLS and has the correct remote timeout and proxy url.
func TestBuildClient(t *testing.T) {
	tests := []struct {
		testName              string
		config                Config
		expectedRemoteTimeout time.Duration
		expectedErrorSuffix   string
	}{
		{
			testName: "Remote Timeout with Proxy URL",
			config: Config{
				ProxyURL:      "123.4.5.6",
				RemoteTimeout: 123 * time.Second,
				TLSConfig: map[string]string{
					"ca_file":              "./ca_cert.pem",
					"insecure_skip_verify": "0",
				},
			},
			expectedRemoteTimeout: 123 * time.Second,
			expectedErrorSuffix:   "proxyconnect tcp: dial tcp :0: connect: can't assign requested address",
		},
		{
			testName: "No Timeout or Proxy URL, InsecureSkipVerify is false",
			config: Config{
				TLSConfig: map[string]string{
					"ca_file":              "./ca_cert.pem",
					"insecure_skip_verify": "0",
				},
			},
			expectedErrorSuffix: "",
		},
		{
			testName: "No Timeout or Proxy URL, InsecureSkipVerify is true",
			config: Config{
				TLSConfig: map[string]string{
					"ca_file":              "./ca_cert.pem",
					"insecure_skip_verify": "1",
				},
			},
			expectedErrorSuffix: "",
		},
	}
	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			// Create and start the TLS server.
			handler := func(rw http.ResponseWriter, req *http.Request) {
				rw.Write([]byte("Successfully received HTTP request!"))
			}
			server := httptest.NewTLSServer(http.HandlerFunc(handler))
			defer server.Close()

			// Create a certicate for the CA from the TLS server. This will be used to
			// verify the test server by the client.
			encodedCACert := server.TLS.Certificates[0].Certificate[0]
			caCertPEM := pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: encodedCACert,
			})
			createFile(caCertPEM, "./ca_cert.pem")
			defer os.Remove("ca_cert.pem")

			// Create an Exporter client and check the timeout.
			exporter := Exporter{
				config: test.config,
			}
			client, err := exporter.buildClient()
			require.Nil(t, err)
			require.Equal(t, client.Timeout, test.expectedRemoteTimeout)

			// Attempt to send the request and verify that the correct error occurred. If
			// an error is expected, the test checks the error string's suffix since the
			// error can contain the server URL, which changes every test.
			_, err = client.Get(server.URL)
			if test.expectedErrorSuffix != "" {
				require.Error(t, err)
				errorSuffix := strings.HasSuffix(err.Error(), test.expectedErrorSuffix)
				require.True(t, errorSuffix)
			} else {
				require.Nil(t, err)
			}
		})
	}
}

// generateCertFiles generates new certificate files from a template that is signed with
// the provided signer certificate and key.
func generateCertFiles(
	template *x509.Certificate,
	signer *x509.Certificate,
	signerKey *rsa.PrivateKey,
	certFilepath string,
	keyFilepath string,
) (*x509.Certificate, *rsa.PrivateKey, error) {
	// Generate a private key for the new certificate. This does not have to be rsa 4096.
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, err
	}

	// Check if a signer key was provided. If not, then use the newly created key.
	if signerKey == nil {
		signerKey = privateKey
	}

	// Create a new certificate in DER encoding.
	encodedCert, err := x509.CreateCertificate(
		rand.Reader, template, signer, privateKey.Public(), signerKey,
	)
	if err != nil {
		return nil, nil, err
	}

	// Write the certificate to the local directory.
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: encodedCert,
	})
	createFile(certPEM, certFilepath)

	// Write the private key to the local directory.
	encodedPrivateKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: encodedPrivateKey,
	})
	createFile(privateKeyPEM, keyFilepath)

	// Parse the newly created certificate so it can be returned.
	cert, err := x509.ParseCertificate(encodedCert)
	if err != nil {
		return nil, nil, err
	}
	return cert, privateKey, nil
}

// generateCACertFiles creates a CA certificate and key in the local directory. This
// certificate is used to sign other certificates.
func generateCACertFiles(certFilepath string, keyFilepath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	// Create a template for CA certificates.
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(123),
		Subject: pkix.Name{
			Organization: []string{"CA Certificate"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(5 * time.Minute),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	// Create the certificate files. CA certificates are used to sign other certificates
	// so it signs itself with its own template and private key during creation.
	cert, privateKey, err := generateCertFiles(
		certTemplate,
		certTemplate,
		nil,
		certFilepath,
		keyFilepath,
	)
	if err != nil {
		return nil, nil, err
	}

	return cert, privateKey, nil
}

// generateServingCertFiles creates a new certificate that a client will check against its
// certificate authority to verify the server. The certificate is signed by a certificate
// authority.
func generateServingCertFiles(
	caCert *x509.Certificate,
	caPrivateKey *rsa.PrivateKey,
	certFilepath string,
	keyFilepath string,
) (*x509.Certificate, *rsa.PrivateKey, error) {
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(456),
		Subject: pkix.Name{
			Organization: []string{"Serving Certificate"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(5 * time.Minute),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}

	// Create the certificate files. The CA certificate is used to sign the new
	// certificate.
	cert, privateKey, err := generateCertFiles(
		certTemplate,
		caCert,
		caPrivateKey,
		"./serving_cert.pem",
		"./serving_key.pem",
	)
	if err != nil {
		return nil, nil, err
	}

	return cert, privateKey, nil
}

// generateClientCertFiles creates a new certificate that a server will check against its
// certificate authority to verify the client. The certificate is signed by a certificate
// authority.
func generateClientCertFiles(
	caCert *x509.Certificate,
	caPrivateKey *rsa.PrivateKey,
	certFilepath string,
	keyFilepath string,
) (*x509.Certificate, *rsa.PrivateKey, error) {
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(789),
		Subject: pkix.Name{
			Organization: []string{"Client Certificate"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(5 * time.Minute),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Create the certificate files. The CA certificate is used to sign the new
	// certificate.
	cert, privateKey, err := generateCertFiles(
		certTemplate,
		caCert,
		caPrivateKey,
		"./client_cert.pem",
		"./client_key.pem",
	)
	if err != nil {
		return nil, nil, err
	}

	return cert, privateKey, nil
}
