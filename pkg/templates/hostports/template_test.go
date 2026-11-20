package hostports

import (
	"fmt"
	"testing"

	ocsAppsV1 "github.com/openshift/api/apps/v1"
	"github.com/stretchr/testify/suite"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"golang.stackrox.io/kube-linter/pkg/templates"
	"golang.stackrox.io/kube-linter/pkg/templates/hostports/internal/params"
)

const (
	templateKey = "host-ports"

	appContainer     = "app"
	sidecarContainer = "sidecar"
	initContainer    = "init"
	debugContainer   = "debugger"
)

func TestHostPorts(t *testing.T) {
	suite.Run(t, new(HostPortsTestSuite))
}

type HostPortsTestSuite struct {
	templates.TemplateTestSuite
	ctx *mocks.MockLintContext
}

func (s *HostPortsTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

// portWithHostPort returns a container port binding a host port under the given protocol.
func portWithHostPort(containerPort, hostPort int32, protocol v1.Protocol) v1.ContainerPort {
	return v1.ContainerPort{
		Name:          fmt.Sprintf("port-%d", hostPort),
		ContainerPort: containerPort,
		HostPort:      hostPort,
		Protocol:      protocol,
	}
}

// portWithoutHostPort returns a container port declaration without any host binding.
func portWithoutHostPort(containerPort int32, protocol v1.Protocol) v1.ContainerPort {
	return v1.ContainerPort{
		Name:          fmt.Sprintf("port-%d", containerPort),
		ContainerPort: containerPort,
		Protocol:      protocol,
	}
}

func hostPortDiagnostic(containerName string, hostPort int32, protocol string) diagnostic.Diagnostic {
	return diagnostic.Diagnostic{
		Message: fmt.Sprintf("host port %d (protocol %s) is exposed in container %q", hostPort, protocol, containerName),
	}
}

// addWorkload registers one object per reachable DeploymentLike form. The mutate function lets the
// caller populate the pod spec of the freshly created object.
func (s *HostPortsTestSuite) addWorkload(kind, name string, mutate func(podSpec *v1.PodSpec)) {
	container := v1.Container{
		Name:  appContainer,
		Image: "myorg/myimage:tag",
		Ports: []v1.ContainerPort{portWithoutHostPort(8080, v1.ProtocolTCP)},
	}
	switch kind {
	case "Deployment":
		obj := &appsV1.Deployment{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	case "DaemonSet":
		obj := &appsV1.DaemonSet{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	case "StatefulSet":
		obj := &appsV1.StatefulSet{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	case "ReplicaSet":
		obj := &appsV1.ReplicaSet{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	case "Pod":
		obj := &v1.Pod{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec)
		s.ctx.AddObject(name, obj)
	case "ReplicationController":
		obj := &v1.ReplicationController{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Template = &v1.PodTemplateSpec{}
		obj.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	case "Job":
		obj := &batchV1.Job{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	case "CronJob":
		obj := &batchV1.CronJob{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.JobTemplate.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.JobTemplate.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	case "DeploymentConfig":
		obj := &ocsAppsV1.DeploymentConfig{ObjectMeta: metaV1.ObjectMeta{Name: name}}
		obj.Spec.Template = &v1.PodTemplateSpec{}
		obj.Spec.Template.Spec.Containers = []v1.Container{container}
		mutate(&obj.Spec.Template.Spec)
		s.ctx.AddObject(name, obj)
	default:
		s.Failf("unknown workload kind", kind)
	}
}

// TestAllDeploymentLikeForms exercises every GroupKind reachable through the DeploymentLike
// object kind: each form gets both a non-triggering fixture (ports but no host binding) and a
// triggering fixture (an explicit, non-zero hostPort).
func (s *HostPortsTestSuite) TestAllDeploymentLikeForms() {
	kinds := []string{
		"Deployment",
		"DaemonSet",
		"StatefulSet",
		"ReplicaSet",
		"Pod",
		"ReplicationController",
		"Job",
		"CronJob",
		"DeploymentConfig",
	}

	expectedDiagnostics := map[string][]diagnostic.Diagnostic{}
	for i, kind := range kinds {
		hostPort := int32(9000 + i)
		cleanName := fmt.Sprintf("%s-clean", kind)
		violatingName := fmt.Sprintf("%s-violating", kind)
		s.addWorkload(kind, cleanName, func(podSpec *v1.PodSpec) {})
		s.addWorkload(kind, violatingName, func(podSpec *v1.PodSpec) {
			podSpec.Containers[0].Ports = []v1.ContainerPort{
				portWithoutHostPort(8080, v1.ProtocolTCP),
				portWithHostPort(8081, hostPort, v1.ProtocolTCP),
			}
		})
		expectedDiagnostics[violatingName] = []diagnostic.Diagnostic{
			hostPortDiagnostic(appContainer, hostPort, "TCP"),
		}
	}

	s.Validate(s.ctx, []templates.TestCase{
		{
			Param:       params.Params{},
			Diagnostics: expectedDiagnostics,
		},
	})
}

// TestOmittedAndZeroHostPortDoesNotTrigger pins the field-default semantics: hostPort is an
// optional int32, so an omitted field and an explicit zero both mean "no host binding" and must
// not produce a diagnostic.
func (s *HostPortsTestSuite) TestOmittedAndZeroHostPortDoesNotTrigger() {
	s.ctx.AddMockDeployment(s.T(), "omitted-host-port")
	s.ctx.ModifyDeployment(s.T(), "omitted-host-port", func(deployment *appsV1.Deployment) {
		deployment.Spec.Template.Spec.Containers = []v1.Container{{
			Name:  appContainer,
			Image: "myorg/myimage:tag",
			Ports: []v1.ContainerPort{
				{Name: "http", ContainerPort: 8080},
				{Name: "metrics", ContainerPort: 9090, Protocol: v1.ProtocolUDP},
			},
		}}
	})

	s.ctx.AddMockDeployment(s.T(), "explicit-zero-host-port")
	s.ctx.ModifyDeployment(s.T(), "explicit-zero-host-port", func(deployment *appsV1.Deployment) {
		deployment.Spec.Template.Spec.Containers = []v1.Container{{
			Name:  appContainer,
			Image: "myorg/myimage:tag",
			Ports: []v1.ContainerPort{
				{Name: "http", ContainerPort: 8080, HostPort: 0, Protocol: v1.ProtocolTCP},
			},
		}}
	})

	s.Validate(s.ctx, []templates.TestCase{
		{
			Param:       params.Params{},
			Diagnostics: map[string][]diagnostic.Diagnostic{},
		},
	})
}

// TestProtocolSemantics covers the multi-protocol shapes: an omitted protocol defaults to TCP
// (same convention as the ports template), while explicit UDP and SCTP entries are reported with
// their declared protocol. Every offending entry yields its own diagnostic.
func (s *HostPortsTestSuite) TestProtocolSemantics() {
	s.ctx.AddMockDeployment(s.T(), "multi-protocol")
	s.ctx.ModifyDeployment(s.T(), "multi-protocol", func(deployment *appsV1.Deployment) {
		deployment.Spec.Template.Spec.Containers = []v1.Container{{
			Name:  appContainer,
			Image: "myorg/myimage:tag",
			Ports: []v1.ContainerPort{
				{Name: "default-proto", ContainerPort: 8080, HostPort: 8080},
				{Name: "udp", ContainerPort: 5000, HostPort: 5000, Protocol: v1.ProtocolUDP},
				{Name: "sctp", ContainerPort: 6000, HostPort: 6000, Protocol: v1.ProtocolSCTP},
			},
		}}
	})

	s.Validate(s.ctx, []templates.TestCase{
		{
			Param: params.Params{},
			Diagnostics: map[string][]diagnostic.Diagnostic{
				"multi-protocol": {
					hostPortDiagnostic(appContainer, 8080, "TCP"),
					hostPortDiagnostic(appContainer, 5000, "UDP"),
					hostPortDiagnostic(appContainer, 6000, "SCTP"),
				},
			},
		},
	})
}

// TestAllContainerClasses verifies that host ports are flagged in init containers and ephemeral
// containers as well, not only in regular containers: AllContainers is the reachable container
// surface for any DeploymentLike pod spec.
func (s *HostPortsTestSuite) TestAllContainerClasses() {
	s.ctx.AddMockDeployment(s.T(), "all-container-classes")
	s.ctx.ModifyDeployment(s.T(), "all-container-classes", func(deployment *appsV1.Deployment) {
		deployment.Spec.Template.Spec.InitContainers = []v1.Container{{
			Name:  initContainer,
			Image: "myorg/init:tag",
			Ports: []v1.ContainerPort{portWithHostPort(7000, 7000, v1.ProtocolTCP)},
		}}
		deployment.Spec.Template.Spec.Containers = []v1.Container{{
			Name:  appContainer,
			Image: "myorg/myimage:tag",
			Ports: []v1.ContainerPort{portWithHostPort(8080, 8080, v1.ProtocolTCP)},
		}, {
			Name:  sidecarContainer,
			Image: "myorg/sidecar:tag",
			Ports: []v1.ContainerPort{portWithoutHostPort(9000, v1.ProtocolTCP)},
		}}
		deployment.Spec.Template.Spec.EphemeralContainers = []v1.EphemeralContainer{{
			EphemeralContainerCommon: v1.EphemeralContainerCommon{
				Name:  debugContainer,
				Image: "myorg/debug:tag",
				Ports: []v1.ContainerPort{portWithHostPort(10000, 10000, v1.ProtocolUDP)},
			},
		}}
	})

	s.Validate(s.ctx, []templates.TestCase{
		{
			Param: params.Params{},
			Diagnostics: map[string][]diagnostic.Diagnostic{
				"all-container-classes": {
					hostPortDiagnostic(initContainer, 7000, "TCP"),
					hostPortDiagnostic(appContainer, 8080, "TCP"),
					hostPortDiagnostic(debugContainer, 10000, "UDP"),
				},
			},
		},
	})
}

// TestSupportedObjectKindsPinsDeploymentLike guards the object-kind scope of the template: the
// host-ports check must remain scoped to the DeploymentLike matcher and keep its GVK coverage
// aligned with the objectkinds registry.
func (s *HostPortsTestSuite) TestSupportedObjectKindsPinsDeploymentLike() {
	template, found := templates.Get(templateKey)
	s.Require().True(found)
	s.ElementsMatch([]string{"DeploymentLike"}, template.SupportedObjectKinds.ObjectKinds)

	matcher, err := objectkinds.ConstructMatcher(objectkinds.DeploymentLike)
	s.Require().NoError(err)

	// Sanity-check the nine GroupKinds the DeploymentLike matcher accepts at runtime.
	for _, gk := range []schema.GroupKind{
		{Group: "apps", Kind: "Deployment"},
		{Group: "apps", Kind: "DaemonSet"},
		{Group: "apps.openshift.io", Kind: "DeploymentConfig"},
		{Group: "apps", Kind: "StatefulSet"},
		{Group: "apps", Kind: "ReplicaSet"},
		{Group: "", Kind: "Pod"},
		{Group: "", Kind: "ReplicationController"},
		{Group: "batch", Kind: "Job"},
		{Group: "batch", Kind: "CronJob"},
	} {
		s.True(matcher.Matches(gk.WithVersion("v1")), "%s must be reachable via DeploymentLike", gk.String())
	}
}
