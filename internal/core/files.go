package core

// RenovateConfig returns a generic renovate.json. Renovate auto-detects every
// package manager, so a single config works for any language — which is why
// file remediation can add it generically (unlike a CI workflow).
func RenovateConfig() []byte {
	return []byte(`{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": ["config:recommended"],
  "dependencyDashboard": true
}
`)
}
