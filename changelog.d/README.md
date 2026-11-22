# Changelog fragments

Each pull request writes its changelog entry into its own file here instead of
editing `## [Unreleased]` in `CHANGELOG.md`. Two pull requests that both edit
that section conflict as soon as one of them merges; two fragment files do not.

## Format

- File name: `<branch-slug>.<section>.md`, lowercase, for example
  `fix-state-file-write.fixed.md`. The branch slug keeps names unique.
- `<section>` selects the heading the entry lands under:

  | Section      | Heading in `CHANGELOG.md` |
  |--------------|---------------------------|
  | `added`      | `### Added`               |
  | `changed`    | `### Changed`             |
  | `deprecated` | `### Deprecated`          |
  | `removed`    | `### Removed`             |
  | `fixed`      | `### Fixed`               |
  | `security`   | `### Security`            |

- Content: one or more top-level bullets (`-` and a space), written as they
  will appear in `CHANGELOG.md`. No heading; the file name selects it. A change
  that belongs under two headings gets two files.

`make changelog-check` checks the names and contents. The pre-commit hook runs
it, and `go test ./...` runs it as a test, so CI refuses a malformed fragment.

## Release

The release pull request runs `make changelog-collect` before it renames
`[Unreleased]`. The command appends every fragment, in file-name order, at the
end of its heading, creates a missing heading in Keep a Changelog order, and
deletes the fragment. With no fragments it changes nothing. The check fails
while fragments remain and the topmost released section has no tag yet,
because those notes would not ship.
