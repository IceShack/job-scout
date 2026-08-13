// Package version holds the one place the release number is written down.
package version

// Version is the release, as semver without a leading "v". It is the
// source of truth: the git tag, the published image tag and the OpenAPI
// document all follow it, and CI refuses a tag that disagrees.
const Version = "1.1.0"
