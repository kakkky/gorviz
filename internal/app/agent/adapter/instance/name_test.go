package instance

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestName(t *testing.T) {
	t.Run("the given name is used as is outside Cloud Run", func(t *testing.T) {
		t.Setenv("K_SERVICE", "")
		got, err := Name(t.Context(), "api")
		require.NoError(t, err)
		assert.Equal(t, "api", got)
	})

	t.Run("Cloud Run instances get different names from their instance IDs", func(t *testing.T) {
		t.Setenv("K_SERVICE", "api")
		ids := map[string]string{"/a": "instance-a", "/b": "instance-b"}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Google", r.Header.Get("Metadata-Flavor"))
			_, _ = w.Write([]byte(ids[r.URL.Path]))
		}))
		defer srv.Close()
		nameOn := func(path, name string) string {
			cloudRunInstanceIDURL = srv.URL + path
			got, err := Name(t.Context(), name)
			require.NoError(t, err)
			return got
		}
		t.Cleanup(func() { cloudRunInstanceIDURL = "http://metadata.google.internal/computeMetadata/v1/instance/id" })

		a, b := nameOn("/a", ""), nameOn("/b", "")
		assert.Regexp(t, `^api/[0-9a-f]{12}$`, a, "the service name is the prefix when -name is omitted")
		assert.NotEqual(t, a, b)
		assert.Equal(t, a, nameOn("/a", ""), "the same instance keeps its name across reconnects")
		assert.Regexp(t, `^front/`, nameOn("/a", "front"), "-name replaces the service name")
	})
}
