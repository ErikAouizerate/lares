// Package lares embeds the coi profile the lares command installs.
//
// The profile and its provisioning script are the sandbox definition: which
// image it is built from, which tool runs in it, and which host directories it
// sees. Keeping them in the binary means `lares install` needs nothing else on
// disk.
package lares

import _ "embed"

// Config is the coi profile declaring the sandbox, written to
// ~/.coi/profiles/lares/config.toml.
//
//go:embed profile/config.toml
var Config []byte

// Provision is the script coi runs to build the sandbox image, written next to
// Config.
//
//go:embed profile/provision.sh
var Provision []byte

// TokenHelper is the script pi runs to read one secret from the sandbox
// secrets file. lares pushes it into the container so the credentials pi reads
// can be resolved at use time instead of being copied into its environment.
//
//go:embed profile/lares-token
var TokenHelper []byte
