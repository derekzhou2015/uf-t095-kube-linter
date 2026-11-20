package hostports

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"golang.stackrox.io/kube-linter/pkg/templates"
	"golang.stackrox.io/kube-linter/pkg/templates/hostports/internal/params"
	"golang.stackrox.io/kube-linter/pkg/templates/util"
	v1 "k8s.io/api/core/v1"
)

func init() {
	templates.Register(check.Template{
		HumanName:   "Host Ports",
		Key:         "host-ports",
		Description: "Flag containers exposing host ports",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{objectkinds.DeploymentLike},
		},
		Parameters:             params.ParamDescs,
		ParseAndValidateParams: params.ParseAndValidate,
		Instantiate: params.WrapInstantiateFunc(func(_ params.Params) (check.Func, error) {
			return util.PerContainerCheck(func(container *v1.Container) []diagnostic.Diagnostic {
				var results []diagnostic.Diagnostic
				for _, port := range container.Ports {
					// hostPort is an optional int32 that defaults to zero when omitted in the YAML.
					// A zero value means the container does not bind a port on the host, so only
					// explicitly declared, non-zero host ports are host port exposures.
					if port.HostPort == 0 {
						continue
					}
					// The k8s protocol defaults to TCP even if not set in the YAML.
					protocol := string(port.Protocol)
					if protocol == "" {
						protocol = "TCP"
					}
					results = append(results, diagnostic.Diagnostic{
						Message: fmt.Sprintf("host port %d (protocol %s) is exposed in container %q",
							port.HostPort, protocol, container.Name),
					})
				}
				return results
			}), nil
		}),
	})
}
