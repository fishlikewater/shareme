package app

type BootstrapSnapshot struct {
	LocalDeviceName string         `json:"localDeviceName"`
	Health          map[string]any `json:"health"`
	Peers           []any          `json:"peers"`
}

type Service interface {
	Bootstrap() BootstrapSnapshot
}
