package cortex

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

var (
	// ErrTwoPasswords is an error for when the YAML file contains both `password` and `password_file`.
	ErrTwoPasswords = fmt.Errorf("Cannot have two passwords in the YAML file")

	// ErrTwoBearerTokens is an error for when the YAML file contains both `bearer_token` and
	// `bearer_token_file`.
	ErrTwoBearerTokens = fmt.Errorf("Cannot have two bearer tokens in the YAML file")
)

// Config contains properties the Exporter uses to export metrics data to Cortex.
type Config struct {
	Endpoint        string            `mapstructure:"url"`
	RemoteTimeout   time.Duration     `mapstructure:"remote_timeout"`
	Name            string            `mapstructure:"name"`
	BasicAuth       map[string]string `mapstructure:"basic_auth"`
	BearerToken     string            `mapstructure:"bearer_token"`
	BearerTokenFile string            `mapstructure:"bearer_token_file"`
	TLSConfig       map[string]string `mapstructure:"tls_config"`
	ProxyURL        string            `mapstructure:"proxy_url"`
	PushInterval    time.Duration     `mapstructure:"push_interval"`
	Headers         map[string]string `mapstructure:"headers"`
	Client          *http.Client
}

// Validate checks a Config struct for missing required properties and property conflicts.
// Additionally, it adds default values to missing properties when there is a default.
func (c *Config) Validate() error {
	// Check for mutually exclusive properties.
	if c.BearerToken != "" && c.BearerTokenFile != "" {
		return ErrTwoBearerTokens
	}
	if c.BasicAuth["password"] != "" && c.BasicAuth["password_file"] != "" {
		return ErrTwoPasswords
	}

	// Add default values for missing properties.
	if c.Endpoint == "" {
		c.Endpoint = "/api/prom/push"
	}
	if c.RemoteTimeout == 0 {
		c.RemoteTimeout = 30 * time.Second
	}
	// Default time interval between pushes for the push controller is 10s.
	if c.PushInterval == 0 {
		c.PushInterval = 10 * time.Second
	}
	if c.Client == nil && c.ProxyURL != "" {
		// Create a custom transport with a proxy URL. This is the same as the http.DefaultTransport
		// other than the proxy.
		parsedProxyURL, err := url.Parse(c.ProxyURL)
		if err != nil {
			return err
		}
		transport := &http.Transport{
			Proxy: http.ProxyURL(parsedProxyURL),
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}

		// Client is the same as http.DefaultClient other than the proxy.
		c.Client = &http.Client{Transport: transport}
	}
	if c.Client == nil {
		c.Client = http.DefaultClient
	}
	return nil
}
