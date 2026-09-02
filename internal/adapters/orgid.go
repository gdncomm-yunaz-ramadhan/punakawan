package adapters

import "strings"

// Adapter ids carry an optional organisation suffix: "atlassian" names
// the host's single configured Atlassian site, "atlassian:gdncomm" names
// one specific one. Both spawn the same adapter program - what differs is
// the credentials handed to that process, so two organisations get two
// processes and can never read each other's data through a shared one.
//
// The suffix is part of the id everywhere an id travels, including the
// durable outbox's AdapterID column, so a write queued against one
// organisation still executes against that organisation after a restart.
const orgSeparator = ":"

// QualifyAdapterID returns the adapter id routing to org. An empty org
// returns base unchanged, which is what every host with one configured
// site keeps using.
func QualifyAdapterID(base, org string) string {
	org = strings.TrimSpace(org)
	if org == "" {
		return base
	}
	return base + orgSeparator + org
}

// SplitAdapterID separates an adapter id into its program and its
// organisation. An id with no suffix reports an empty org.
func SplitAdapterID(id string) (base, org string) {
	base, org, found := strings.Cut(id, orgSeparator)
	if !found {
		return id, ""
	}
	return base, org
}
