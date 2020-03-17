package nomad

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/hashicorp/nomad/nomad/structs"
)

// jobExposeHook implements a job Mutating and Validating admission controller.
type jobExposeHook struct{}

func (jobExposeHook) Name() string {
	return "expose"
}

func (jobExposeHook) Mutate(job *structs.Job) (*structs.Job, []error, error) {
	fmt.Printf("jeh.Mutate")
	// create a port for each compatible consul service check, if expose.checks
	// is enabled

	// tbd: We could potentially have done the extrapolation in the consul client
	// code, closer to sync time. Would that be better?

	for _, tg := range job.TaskGroups {
		for _, s := range tg.Services {
			if serviceEnablesExposeChecks(s) {
				for _, check := range s.Checks {
					// create an expose path for each check that is compatible
					_, port := tg.Networks.Port(s.PortLabel)
					if ePath := exposePathForCheck(port, check); ePath != nil {
						fmt.Printf("epath: %s, service: %s\n", ePath, s.Name)
						s.Connect.SidecarService.Proxy.Expose.Paths = append(
							s.Connect.SidecarService.Proxy.Expose.Paths,
							*ePath,
						)
					}
				}
			}
		}
	}

	return job, nil, nil
}

func (jobExposeHook) Validate(job *structs.Job) ([]error, error) {
	fmt.Printf("jeh.Validate")
	// make sure expose config exists only along with a namespaced (bridge mode)
	// network

	for _, tg := range job.TaskGroups {
		if tgEnablesExpose(tg) {
			if mode, group, ok := tgUsesBridgeNetwork(tg); !ok {
				return nil, errors.Errorf("expose configuration requires bridge network, found %s in task group %s", mode, group)
			}
		}
	}

	return nil, nil
}

// exposePathForCheck extrapolates the necessary expose path configuration for
// the given consul service check. If the check is not compatible, nil is
// returned instead.
func exposePathForCheck(port int, check *structs.ServiceCheck) *structs.ConsulExposePath {
	if !checkIsExposable(check) {
		return nil
	}
	return &structs.ConsulExposePath{
		Path:          check.Path,
		Protocol:      check.Protocol,
		LocalPathPort: port,
		ListenerPort:  check.PortLabel,
	}
}

func checkIsExposable(check *structs.ServiceCheck) bool {
	switch {
	case check.Protocol == "grpc":
		return true
	case check.Protocol == "http":
		return true
	default:
		return false
	}
}

func tgEnablesExpose(tg *structs.TaskGroup) bool {
	for _, s := range tg.Services {
		if serviceEnablesExpose(s) {
			return true
		}
	}
	return false
}

func serviceEnablesExpose(s *structs.Service) bool {
	exposeConfig := serviceExposeConfig(s)
	if exposeConfig == nil {
		return false
	}
	return exposeConfig.Checks || len(exposeConfig.Paths) > 0
}

func serviceEnablesExposeChecks(s *structs.Service) bool {
	exposeConfig := serviceExposeConfig(s)
	if exposeConfig == nil {
		return false
	}
	return exposeConfig.Checks
}

func serviceExposeConfig(s *structs.Service) *structs.ConsulExposeConfig {
	if s == nil {
		return nil
	}

	if s.Connect == nil {
		return nil
	}

	if s.Connect.SidecarService == nil {
		return nil
	}

	if s.Connect.SidecarService.Proxy == nil {
		return nil
	}

	return s.Connect.SidecarService.Proxy.Expose
}

func tgUsesBridgeNetwork(tg *structs.TaskGroup) (string, string, bool) {
	mode := tg.Networks[0].Mode
	return mode, tg.Name, tg.Networks[0].Mode == "bridge"
}

const (
	// -1 is a sentinel value to instruct the
	// scheduler to map the host's dynamic port to
	// the same port in the netns.
	portMapSentinel = -1
)

func makePort(label string, networks structs.Networks) {
	for _, p := range networks[0].DynamicPorts {
		if p.Label == label {
			return // what about to=0
		}
		networks[0].DynamicPorts = append(networks[0].DynamicPorts, structs.Port{
			Label: label,
			To:    portMapSentinel,
		})
	}
}
