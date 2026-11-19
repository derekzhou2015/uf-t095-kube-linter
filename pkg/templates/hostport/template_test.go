package hostport

import (
	"fmt"
	"testing"

	ocsAppsV1 "github.com/openshift/api/apps/v1"
	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	"golang.stackrox.io/kube-linter/pkg/templates/hostport/internal/params"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const containerName = "app"

func TestHostPort(t *testing.T) {
	suite.Run(t, new(HostPortTestSuite))
}

type HostPortTestSuite struct {
	templates.TemplateTestSuite

	ctx      *mocks.MockLintContext
	expected map[string][]diagnostic.Diagnostic
}

func (s *HostPortTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
	s.expected = map[string][]diagnostic.Diagnostic{}
}

func (s *HostPortTestSuite) validate() {
	s.Validate(s.ctx, []templates.TestCase{
		{
			Param:                    params.Params{},
			Diagnostics:              s.expected,
			ExpectInstantiationError: false,
		},
	})
}

func hostPortDiagnostic(containerPort int, protocol string, hostPort int) diagnostic.Diagnostic {
	return diagnostic.Diagnostic{
		Message: fmt.Sprintf("port %d/%s in container %q is bound to host port %d.", containerPort, protocol, containerName, hostPort),
	}
}

func (s *HostPortTestSuite) expectDiagnostics(name string, diags ...diagnostic.Diagnostic) {
	s.expected[name] = append(s.expected[name], diags...)
}

func (s *HostPortTestSuite) addDeployment(name string, ports []v1.ContainerPort) {
	s.ctx.AddMockDeployment(s.T(), name)
	s.ctx.ModifyDeployment(s.T(), name, func(deployment *appsV1.Deployment) {
		deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, v1.Container{
			Name:  containerName,
			Ports: ports,
		})
	})
}

func (s *HostPortTestSuite) addDeploymentConfig(name string, ports []v1.ContainerPort) {
	s.ctx.AddMockDeploymentConfig(s.T(), name)
	s.ctx.ModifyDeploymentConfig(s.T(), name, func(deploymentConfig *ocsAppsV1.DeploymentConfig) {
		if deploymentConfig.Spec.Template == nil {
			deploymentConfig.Spec.Template = &v1.PodTemplateSpec{}
		}
		deploymentConfig.Spec.Template.Spec.Containers = append(deploymentConfig.Spec.Template.Spec.Containers, v1.Container{
			Name:  containerName,
			Ports: ports,
		})
	})
}

func (s *HostPortTestSuite) addCronJob(name string, ports []v1.ContainerPort) {
	s.ctx.AddMockCronJob(s.T(), name)
	s.ctx.ModifyCronJob(s.T(), name, func(cronJob *batchV1.CronJob) {
		cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers = append(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers, v1.Container{
			Name:  containerName,
			Ports: ports,
		})
	})
}

func (s *HostPortTestSuite) addPod(name string, ports []v1.ContainerPort) {
	s.ctx.AddObject(name, &v1.Pod{
		ObjectMeta: metaV1.ObjectMeta{Name: name},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: containerName, Ports: ports},
			},
		},
	})
}

// TestExplicitHostPortTriggersAcrossObjectKinds walks the three distinct pod-spec
// extraction paths: Deployment/DeploymentConfig (Spec.Template via reflect), CronJob
// (Spec.JobTemplate.Spec.Template special case) and Pod (Spec special case).
func (s *HostPortTestSuite) TestExplicitHostPortTriggersAcrossObjectKinds() {
	s.addDeployment("deployment-host-port", []v1.ContainerPort{
		{ContainerPort: 8080, HostPort: 8080, Protocol: v1.ProtocolTCP},
	})
	s.addDeploymentConfig("deploymentconfig-host-port", []v1.ContainerPort{
		{ContainerPort: 8080, HostPort: 8080, Protocol: v1.ProtocolTCP},
	})
	s.addCronJob("cronjob-host-port", []v1.ContainerPort{
		{ContainerPort: 8080, HostPort: 8080, Protocol: v1.ProtocolTCP},
	})
	s.addPod("pod-host-port", []v1.ContainerPort{
		{ContainerPort: 8080, HostPort: 8080, Protocol: v1.ProtocolTCP},
	})

	s.expectDiagnostics("deployment-host-port", hostPortDiagnostic(8080, "TCP", 8080))
	s.expectDiagnostics("deploymentconfig-host-port", hostPortDiagnostic(8080, "TCP", 8080))
	s.expectDiagnostics("cronjob-host-port", hostPortDiagnostic(8080, "TCP", 8080))
	s.expectDiagnostics("pod-host-port", hostPortDiagnostic(8080, "TCP", 8080))
	s.validate()
}

// TestOmittedHostPortDoesNotTrigger covers entries with no ports at all, and entries
// that declare only containerPort.
func (s *HostPortTestSuite) TestOmittedHostPortDoesNotTrigger() {
	s.addDeployment("deployment-no-host-port", []v1.ContainerPort{
		{ContainerPort: 8080, Protocol: v1.ProtocolTCP},
	})
	s.addDeploymentConfig("deploymentconfig-no-host-port", []v1.ContainerPort{
		{ContainerPort: 8080},
	})
	s.addCronJob("cronjob-no-ports", nil)
	s.addPod("pod-no-ports", nil)
	s.validate()
}

// TestHostNetworkWithoutHostPortDoesNotTrigger pins the boundary with the host-network
// check: sharing the host namespace implicitly occupies host ports, but that verdict
// belongs to host-network, so this check must not infer anything from hostNetwork.
func (s *HostPortTestSuite) TestHostNetworkWithoutHostPortDoesNotTrigger() {
	s.ctx.AddMockDeployment(s.T(), "deployment-host-network")
	s.ctx.ModifyDeployment(s.T(), "deployment-host-network", func(deployment *appsV1.Deployment) {
		deployment.Spec.Template.Spec.HostNetwork = true
		deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, v1.Container{
			Name:  containerName,
			Ports: []v1.ContainerPort{{ContainerPort: 8080, Protocol: v1.ProtocolTCP}},
		})
	})
	s.validate()
}

// TestMultiProtocolEntries covers the same containerPort declared once per protocol:
// each hostPort-bearing entry is reported on its own, and a missing protocol means TCP.
func (s *HostPortTestSuite) TestMultiProtocolEntries() {
	s.addDeployment("deployment-multi-protocol", []v1.ContainerPort{
		{ContainerPort: 53, HostPort: 53, Protocol: v1.ProtocolTCP},
		{ContainerPort: 53, HostPort: 53, Protocol: v1.ProtocolUDP},
		{ContainerPort: 5353, HostPort: 5353, Protocol: v1.ProtocolSCTP},
	})
	s.expectDiagnostics("deployment-multi-protocol",
		hostPortDiagnostic(53, "TCP", 53),
		hostPortDiagnostic(53, "UDP", 53),
		hostPortDiagnostic(5353, "SCTP", 5353),
	)
	s.validate()
}

func (s *HostPortTestSuite) TestDefaultProtocolIsTCP() {
	s.addDeployment("deployment-default-protocol", []v1.ContainerPort{
		{ContainerPort: 8080, HostPort: 8080},
	})
	s.expectDiagnostics("deployment-default-protocol", hostPortDiagnostic(8080, "TCP", 8080))
	s.validate()
}

// TestMixedEntriesReportOnlyHostPortOnes ensures entries without hostPort in an
// otherwise violating container stay silent.
func (s *HostPortTestSuite) TestMixedEntriesReportOnlyHostPortOnes() {
	s.addDeployment("deployment-mixed", []v1.ContainerPort{
		{ContainerPort: 8080, Protocol: v1.ProtocolTCP},
		{ContainerPort: 8443, HostPort: 8443, Protocol: v1.ProtocolTCP},
		{ContainerPort: 9090, Protocol: v1.ProtocolTCP},
	})
	s.expectDiagnostics("deployment-mixed", hostPortDiagnostic(8443, "TCP", 8443))
	s.validate()
}

// TestInitContainerIsChecked documents that init containers are covered, matching
// PerContainerCheck's semantics used by the neighbouring port checks.
func (s *HostPortTestSuite) TestInitContainerIsChecked() {
	s.ctx.AddMockDeployment(s.T(), "deployment-init-container")
	s.ctx.ModifyDeployment(s.T(), "deployment-init-container", func(deployment *appsV1.Deployment) {
		deployment.Spec.Template.Spec.InitContainers = append(deployment.Spec.Template.Spec.InitContainers, v1.Container{
			Name:  "init",
			Ports: []v1.ContainerPort{{ContainerPort: 7000, HostPort: 7000, Protocol: v1.ProtocolTCP}},
		})
	})
	s.expected["deployment-init-container"] = []diagnostic.Diagnostic{
		{Message: `port 7000/TCP in container "init" is bound to host port 7000.`},
	}
	s.validate()
}
