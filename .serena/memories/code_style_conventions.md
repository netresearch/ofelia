# Code Style and Conventions for Ofelia

## Go Version
- Go 1.25 with toolchain go1.25.0

## Code Organization
- Package names match directory names
- Main entry point in `ofelia.go`
- Clear separation: `/core`, `/cli`, `/middlewares`, `/web`
- Test files alongside source files (`*_test.go`)

## Naming Conventions
- **Exported types/functions**: PascalCase (e.g., `Scheduler`, `NewScheduler`)
- **Private types/functions**: camelCase (e.g., `buildCommand`, `jobWrapper`)
- **Constants**: PascalCase or UPPER_SNAKE_CASE
- **Interfaces**: Named with "-er" suffix when appropriate (e.g., `Logger`)
- **Errors**: Start with "Err" prefix (e.g., `ErrEmptyScheduler`)

## Error Handling
- Use `fmt.Errorf` with `%w` for error wrapping
- Define package-level error variables for common errors
- Always check and handle errors explicitly
- Return errors rather than panic (except one case in web/server.go)

## Testing
- Use `gopkg.in/check.v1` testing framework
- Test suites with setup/teardown methods
- Integration tests in `*_integration_test.go`
- Parallel tests where appropriate
- Mock Docker client for testing

## Documentation
- Comments for all exported types and functions
- Use `//` for single-line comments
- Document complex logic inline
- README.md for user documentation

## Formatting Rules (enforced by golangci-lint)
- gofmt, goimports, gofumpt for formatting
- Line length limit: 140 characters
- Import grouping: standard, default, project-specific
- No fmt.Print* in production code

## Linting Configuration
- Comprehensive linter set via .golangci.yml
- Cyclomatic complexity limit: 15
- Error wrapping required
- Context checking enforced
- Parallel test requirements

## Patterns
- Middleware pattern for cross-cutting concerns
- Interface-based design for extensibility
- Dependency injection for testability
- Proper mutex usage for concurrency safety