package diagnostics

func BuildHealthSnapshot(localAPIReady bool, agentPort int, discovery string) map[string]any {
	status := "ok"
	if !localAPIReady {
		status = "degraded"
	}

	return map[string]any{
		"status":        status,
		"localAPIReady": localAPIReady,
		"agentPort":     agentPort,
		"discovery":     discovery,
	}
}
