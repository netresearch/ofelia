# Running Tests

Unit tests can be run without Docker. Integration tests require either a running
Docker daemon or the Docker test server.

## Unit tests

```sh
go test ./...
```

## Integration tests

Start Docker and run the suite with the `integration` build tag:

```sh
go test -tags=integration ./...
```

The CI workflow executes unit tests first and runs the integration tests in a
separate step.
