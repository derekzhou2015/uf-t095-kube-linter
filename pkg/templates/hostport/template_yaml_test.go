package hostport

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	"golang.stackrox.io/kube-linter/pkg/templates/hostport/internal/params"
)

// TestHostPortFormsFromYAML walks the whole value chain for the shapes that only
// exist on disk: an omitted hostPort, an explicit hostPort: 0, hostNetwork: true
// without a hostPort, and multi-protocol entries with the protocol left out.
func TestHostPortFormsFromYAML(t *testing.T) {
	lintCtxs, err := lintcontext.CreateContexts(nil, "testdata/host-port-forms.yaml")
	require.NoError(t, err)
	require.Len(t, lintCtxs, 1)

	template, found := templates.Get(templateKey)
	require.True(t, found)

	checkFunc, err := template.Instantiate(params.Params{})
	require.NoError(t, err)

	actual := make(map[string][]string)
	for _, lintCtx := range lintCtxs {
		for _, obj := range lintCtx.Objects() {
			for _, diag := range checkFunc(lintCtx, obj) {
				actual[obj.K8sObject.GetName()] = append(actual[obj.K8sObject.GetName()], diag.Message)
			}
		}
	}
	for _, messages := range actual {
		sort.Strings(messages)
	}

	expected := map[string][]string{
		"pod-host-port": {
			`port 5432/TCP in container "app" is bound to host port 5432.`,
		},
		"multi-protocol": {
			`port 53/TCP in container "app" is bound to host port 53.`,
			`port 53/UDP in container "app" is bound to host port 53.`,
		},
	}
	for _, messages := range expected {
		sort.Strings(messages)
	}

	assert.Equal(t, expected, actual)

	// These decode to no host port binding and must stay silent.
	for _, name := range []string{"no-host-port", "zero-host-port", "host-network-no-host-port"} {
		assert.NotContains(t, actual, name)
	}
}
