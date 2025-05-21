# Running Tests

The unit tests expect Go to be installed and a Docker daemon available. The CI pipeline starts Docker before running the test suite.

1. Install Docker and make sure the daemon is running.
2. Fetch dependencies and run the tests:

```sh
go test ./...
```

Some tests start a mocked Docker server and do not require network access, but Docker must be present so the client library works correctly.
