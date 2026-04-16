/**
 * @type {import('semantic-release').GlobalConfig}
 *
 * Release discipline sits with maintainers at merge time. Contributors write
 * conventional commits on their feature branches; the squash-merge PR title
 * becomes the commit that semantic-release reads on main. The CI pr-title job
 * enforces the format before merge, so this config can trust the commit log.
 *
 * For this Go project, semantic-release owns versioning, tagging, changelog,
 * and the release-notes commit back to main. goreleaser (triggered by the
 * resulting vX.Y.Z tag) owns binary builds, GitHub Release asset upload,
 * and the homebrew-tap formula bump PR.
 */
module.exports = {
  branches: ["main"],
  tagFormat: "v${version}",
  plugins: [
    [
      "@semantic-release/commit-analyzer",
      {
        preset: "conventionalcommits",
        releaseRules: [
          { type: "feat", release: "minor" },
          { type: "fix", release: "patch" },
          { type: "perf", release: "patch" },
          { type: "refactor", release: "patch" },
          { type: "revert", release: "patch" },
        ],
      },
    ],
    [
      "@semantic-release/release-notes-generator",
      {
        preset: "conventionalcommits",
        presetConfig: {
          types: [
            { type: "feat", section: "Features" },
            { type: "fix", section: "Bug Fixes" },
            { type: "perf", section: "Performance Improvements" },
            { type: "refactor", section: "Code Refactoring" },
            { type: "docs", section: "Documentation" },
            { type: "style", section: "Styling", hidden: true },
            { type: "test", section: "Tests", hidden: true },
            { type: "build", section: "Build System", hidden: true },
            { type: "ci", section: "CI/CD", hidden: true },
            { type: "chore", section: "Miscellaneous", hidden: true },
          ],
        },
      },
    ],
    [
      "@semantic-release/changelog",
      {
        changelogFile: "CHANGELOG.md",
      },
    ],
    // Commit the updated CHANGELOG.md back to main before goreleaser picks up
    // the tag. No package.json to bump — this is a Go project.
    [
      "@semantic-release/git",
      {
        assets: ["CHANGELOG.md"],
        message:
          "chore(release): ${nextRelease.version}\n\n${nextRelease.notes}",
      },
    ],
    // Create the GitHub Release stub (release notes body, tag). goreleaser
    // will upload the binary assets when it runs on the resulting vX.Y.Z tag.
    "@semantic-release/github",
  ],
};
