<!-- docdaemon ts:2026-10-29T20:35:49Z hash:e02a01b92af90999 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes Deployments and Service for the Vertical Pod Autoscaler (VPA) components, vendored from upstream and templated for dynamic configuration.

The template renders three `Deployment` resources (`vpa-recommender`, `vpa-updater`, `vpa-admission-controller`) and one `Service` (`vpa-webhook`) when the `installVpaController` helper returns true. It preserves upstream security contexts, probes, and resource structures while injecting namespace, image tags, and specific argument overrides via Helm values.

Key customizations include:
- **Updater Min Replicas**: The `--min-replicas` argument is overridden to match `.Values.verticalPodAutoscaler.updater.minReplicas`. This defaults to 1 to ensure the updater can resize single-replica workloads, correcting the upstream default of 2 which would skip such targets.
- **TLS Configuration**: The admission controller mounts a `vpa-tls-certs` secret to handle webhook certificate reloading automatically.
- **Image & Resource Templating**: Container images and resource requests/limits are pulled from `.Values.verticalPodAutoscaler`, allowing operators to customize versions and constraints without modifying the core manifest structure.