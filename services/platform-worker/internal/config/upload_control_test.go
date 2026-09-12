package config

import (
	"strings"
	"testing"
	"time"
)

func TestUploadQueueConfigurationFailsClosed(t *testing.T) {
	base := Config{Region: "japan", Capabilities: capabilitiesFor("japan"), DatabaseURL: "postgres://test", DisplayRevalidateURL: "http://display/revalidate", CacheHMACSecret: strings.Repeat("c", 40), MediaDeleteURL: "http://media/delete", MediaHMACSecret: strings.Repeat("m", 40), PollInterval: time.Second, LeaseDuration: 30 * time.Second,
		MediaUploadQueueMode: "dispatch", MediaUploadControlURL: "https://media.test", MediaUploadControlSecret: strings.Repeat("u", 40)}
	// No observer still permits emergency pause; QueueRunner denies resume.
	if e := base.Validate(); e != nil {
		t.Fatal("valid pause-only configuration refused", e)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"region", func(c *Config) { c.Region = "beijing" }},
		{"mode", func(c *Config) { c.MediaUploadQueueMode = "on" }},
		{"shared_secret", func(c *Config) { c.MediaUploadControlSecret = c.MediaHMACSecret }},
		{"missing_secret", func(c *Config) { c.MediaUploadControlSecret = "" }},
		{"missing_url", func(c *Config) { c.MediaUploadControlURL = "" }},
		{"credentials", func(c *Config) { c.MediaUploadControlURL = "https://user:password@media.test" }},
		{"path", func(c *Config) { c.MediaUploadControlURL = "https://media.test/v1/delete" }},
		{"query", func(c *Config) { c.MediaUploadControlURL = "https://media.test?key=secret" }},
		{"plaintext", func(c *Config) { c.MediaUploadControlURL = "http://media.test" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.change(&c)
			if c.Validate() == nil {
				t.Fatal("unsafe queue configuration accepted")
			}
		})
	}
	c := base
	c.MediaUploadControlURL = "http://media.test"
	c.MediaUploadAllowHTTP = true
	if c.Validate() != nil {
		t.Fatal("explicit private HTTP opt-in refused")
	}
	t.Setenv("MEDIA_UPLOAD_QUEUE_MODE", "")
	t.Setenv("MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "")
	if c := FromEnv(); c.MediaUploadQueueMode != "off" || c.MediaUploadAllowHTTP {
		t.Fatal("queue not default off")
	}
	t.Setenv("MEDIA_UPLOAD_QUEUE_MODE", "dispatch")
	t.Setenv("MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "true")
	t.Setenv("MEDIA_UPLOAD_CONTROL_URL", base.MediaUploadControlURL)
	t.Setenv("MEDIA_UPLOAD_CONTROL_SECRET", base.MediaUploadControlSecret)
	if c := FromEnv(); c.MediaUploadQueueMode != "dispatch" || !c.MediaUploadAllowHTTP || c.MediaUploadControlURL != base.MediaUploadControlURL || c.MediaUploadControlSecret != base.MediaUploadControlSecret {
		t.Fatal("control configuration not read")
	}
}
