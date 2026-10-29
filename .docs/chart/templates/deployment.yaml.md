<!-- docdaemon ts:2026-10-29T20:35:48Z hash:494e3de617177844 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This Helm template defines the Kubernetes Deployment for the `weft-operator` control plane pod.

The deployment configures a single replica set (scaled via `.Values.replicaCount`) running the operator container. It injects critical environment variables to configure internal behavior: `WEFT_OPERATOR_IMAGE` and `WEFT_IMAGE` point to container registries, while `DEFAULT_REFLECTOR_*` settings enable the cluster-wide DNS reflector feature introduced in recent updates. The operator exposes health probes on port 8081 (`/healthz`, `/readyz`) and a webhook listener on port 9443.

Security is handled by mounting an `emptyDir` volume at `/tmp/k8s-webhook-server/serving-certs` for temporary certificate storage, ensuring the root filesystem remains read-only. The pod respects standard scheduling constraints like `nodeSelector`, `affinity`, and `tolerations`. Service account and image pull secrets are applied as defined in values.