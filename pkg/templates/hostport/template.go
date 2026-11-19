package hostport

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"golang.stackrox.io/kube-linter/pkg/templates"
	"golang.stackrox.io/kube-linter/pkg/templates/hostport/internal/params"
	"golang.stackrox.io/kube-linter/pkg/templates/util"
	v1 "k8s.io/api/core/v1"
)

const (
	templateKey = "host-port"
)

func init() {
	templates.Register(check.Template{
		HumanName:   "Host Port",
		Key:         templateKey,
		Description: "Flag container ports that are bound to a host port",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{objectkinds.DeploymentLike},
		},
		Parameters:             params.ParamDescs,
		ParseAndValidateParams: params.ParseAndValidate,
		Instantiate: params.WrapInstantiateFunc(func(_ params.Params) (check.Func, error) {
			// Only explicit hostPort declarations are judged here. A pod that shares the
			// host's network namespace implicitly occupies host ports for all of its
			// container ports, but reporting that is the host-network check's job, so
			// no inference from hostNetwork is made here.
			return util.PerContainerCheck(func(container *v1.Container) []diagnostic.Diagnostic {
				var results []diagnostic.Diagnostic
				for _, port := range container.Ports {
					// An unset or zero hostPort does not bind the container port to the host.
					if port.HostPort == 0 {
						continue
					}
					// The k8s protocol defaults to TCP even if not set in the YAML.
					protocol := string(port.Protocol)
					if protocol == "" {
						protocol = "TCP"
					}
					results = append(results, diagnostic.Diagnostic{
						Message: fmt.Sprintf("port %d/%s in container %q is bound to host port %d.",
							port.ContainerPort, protocol, container.Name, port.HostPort),
					})
				}
				return results
			}), nil
		}),
	})
}
