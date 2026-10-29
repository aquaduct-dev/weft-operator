<!-- docdaemon ts:2026-10-29T20:36:01Z hash:943aec4e85580b78 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains tests verifying that the WeftServer reconciler caches and reuses a single Weft client per connection string to prevent resource leaks.

### Purpose
The `TestWeftClientForReusesClientPerConnString` test ensures that the `WeftServerReconciler` does not create a new HTTP transport and TLS connection for every status refresh cycle. Without this caching, each refresh would leak one idle connection and associated goroutines, leading to significant memory and connection exhaustion over time.

### Key Components

*   **`countingClient`**: A mock implementation of `WeftClient` used solely to track instantiation counts without performing actual network operations. It satisfies the interface by returning empty results for `ListTunnels`.
*   **`made` map**: Tracks how many times a client factory is invoked for specific connection strings. This allows the test to verify that the cache hits (reuse) or misses (creation).
*   **`WeftServerReconciler`**: The system under test. It is initialized with a custom `ClientFactory` that increments the counter in `made` whenever a new client is requested.

### Test Logic

1.  **Cache Hit Verification**: The test calls `weftClientFor` 100 times with the same URL (`weft://s@a:9092`). It asserts that the underlying factory was only called once, confirming that subsequent calls retrieve the cached instance.
2.  **Cache Isolation**: It verifies that a different URL (`weft://s@b:9092`) results in a separate client instance, ensuring keys are distinct per connection string.
3.  **Eviction and Rebuild**: The test explicitly forgets the client for the first URL using `forgetWeftClient`. It then calls `weftClientFor` again for that URL and asserts that the factory count increases to 2, proving that forgotten clients are properly rebuilt upon next access.