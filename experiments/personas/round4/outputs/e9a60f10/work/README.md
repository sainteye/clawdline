# ordersvc

Serves `GET /report`: every order with its customer name, a cleaned note, the grand total, and
the sorted lists of distinct tags and customers. `store.go` stands in for the database; each
store call costs one round trip.

    go run .                 # listens on $ADDR (default 127.0.0.1:8081)
    go test ./...
    go run ./loadtest        # load test (uses $ADDR too) against a running server (-n requests, -c concurrency)
