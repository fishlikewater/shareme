package diagnostics

type Report struct {
	AgentTCPPort        int    `json:"agentTcpPort"`
	DiscoveryUDPPort    int    `json:"discoveryUdpPort"`
	LastConnectionError string `json:"lastConnectionError"`
}

func BuildReport(agentTCPPort int, discoveryUDPPort int, lastConnectionError string) Report {
	return Report{
		AgentTCPPort:        agentTCPPort,
		DiscoveryUDPPort:    discoveryUDPPort,
		LastConnectionError: lastConnectionError,
	}
}
