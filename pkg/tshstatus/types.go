package tshstatus

import (
	"time"
)

// Status represents the output from `tsh status`
type Status struct {
	Active *Profile `json:"active,omitempty"`
}

// Profile represents a teleport profile
type Profile struct {
	ProfileURL        string    `json:"profile_url"`
	Username          string    `json:"username"`
	Cluster           string    `json:"cluster"`
	Roles             []string  `json:"roles"`
	Traits            Traits    `json:"traits"`
	Logins            []string  `json:"logins"`
	KubernetesEnabled bool      `json:"kubernetes_enabled"`
	KubernetesCluster string    `json:"kubernetes_cluster"`
	KubernetesUsers   []string  `json:"kubernetes_users"`
	KubernetesGroups  []string  `json:"kubernetes_groups"`
	ValidUntil        time.Time `json:"valid_until"`
	Extensions        []string  `json:"extensions"`
}

// Traits represents the traits assigned to a user
type Traits struct {
	GithubTeams      []string `json:"github_teams"`
	KubernetesGroups []string `json:"kubernetes_groups"`
	KubernetesUsers  []string `json:"kubernetes_users"`
	Logins           []string `json:"logins"`
}
