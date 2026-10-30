<!-- docdaemon ts:2026-10-30T02:59:49Z hash:7776795950ac63f9 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the `weftgateway` controller's logic that converts HTTP route filters into URL fragments and selects listeners based on parent references.

### Purpose
The tests verify two core behaviors of the Gateway API integration:
1. **Filter Serialization**: Ensures that `HTTPRouteFilter` objects (specifically header modifiers) are correctly serialized into a URL fragment string, handling special characters like spaces and percent signs via URL encoding.
2. **Listener Selection**: Validates that `parentRefsSelectListener` correctly determines which listeners in a Gateway should be active based on pinned section names and namespace matching.

### Key Functions Tested
- **`httpFiltersToFragment`**: Converts a slice of `gatewayv1.HTTPRouteFilter` into a query-string-like fragment. The test `TestHTTPFiltersToFragment_ValuesSurviveURLDecoding` confirms that values survive the round-trip through `url.Parse`, ensuring the decoded fragment matches the original input values.
- **`parentRefsSelectListener`**: Determines if a specific listener is selected by checking `ParentReference` attributes against the Gateway's name, namespace, and optional `SectionName`. The test `TestParentRefsSelectListener` covers cases including unpinned references, pinning to specific sections, multiple pins, and mismatched namespaces or gateways.