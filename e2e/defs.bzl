"""Shared macros for the weft-operator firecracker e2e tests."""

load("@rules_firecracker//firecracker:defs.bzl", "firecracker_k3s_test")
load("@rules_oci//oci:defs.bzl", "oci_image", "oci_load")
load("@rules_pkg//:pkg.bzl", "pkg_tar")

# Preloaded into every e2e VM: what k3s itself runs (pause, CoreDNS, local-path
# storage), the operator and weft.
COMMON_IMAGES = {
    "//e2e/images:pause_tar": "pause.tar",
    "//e2e/images:coredns_tar": "coredns.tar",
    "//e2e/images:local_path_provisioner_tar": "local-path-provisioner.tar",
    "//e2e/images:busybox_tar": "busybox.tar",
    "//e2e/images:operator_tar": "operator.tar",
    "//e2e/images:weft_tar": "weft.tar",
}

# Installed on every e2e VM's filesystem.
COMMON_FILES = {
    "//e2e/images:gateway_api_crds": "/etc/e2e/gateway-api-crds.yaml",
    "//e2e/images:node_ca_bundle": "/etc/ssl/certs/ca-certificates.crt",
}

def e2e_binary_image(name, binary, tag):
    """An alpine image with `binary` at /e2e, as `:<name>_tar`.

    Scenario binaries double as the in-pod helpers (echo servers, fake APIs,
    clients), so a scenario preloads an image of its own binary.

    Args:
      name: target name prefix.
      binary: the static go_binary.
      tag: the repo tag pods reference (use imagePullPolicy Never/IfNotPresent).
    """
    pkg_tar(
        name = name + "_layer",
        files = {binary: "e2e"},
        mode = "0755",
    )
    oci_image(
        name = name + "_image",
        base = "@alpine_linux_amd64",
        entrypoint = ["/e2e"],
        tars = [":" + name + "_layer"],
    )
    oci_load(
        name = name + "_load",
        image = ":" + name + "_image",
        repo_tags = [tag],
    )
    native.filegroup(
        name = name + "_tar",
        srcs = [":" + name + "_load"],
        output_group = "tarball",
    )

def weft_e2e_test(
        name,
        scenario,
        test_binary,
        extra_images = {},
        extra_files = {},
        extra_execs = {},
        extra_manifests = {},
        timeout_seconds = 1800,
        vcpus = 4,
        mem_mib = 4096,
        size_mb = 8192,
        **kwargs):
    """A firecracker_k3s_test with COMMON_IMAGES and COMMON_FILES preloaded.

    Run with `bazel test --config=kvm`. The scenario binary receives
    --scenario=/etc/manifests/<scenario>.yaml; an empty scenario manifest is
    generated when extra_manifests does not provide one.

    Args:
      name: test name.
      scenario: scenario name (selects the step list in the binary).
      test_binary: static guest binary using //e2e/e2ekit.
      extra_images: more {image tarball: file name} to preload.
      extra_files: more {label: absolute guest path} (0644).
      extra_execs: more {label: absolute guest path} (0755).
      extra_manifests: {label: file name} applied by the VM init.
      timeout_seconds: VM deadline.
      vcpus: VM vCPUs.
      mem_mib: VM memory.
      size_mb: VM rootfs size.
      **kwargs: passed through to firecracker_k3s_test.
    """
    manifests = dict(extra_manifests)
    if scenario + ".yaml" not in manifests.values():
        native.genrule(
            name = name + "_scenario_yaml",
            outs = [name + "_scenario/" + scenario + ".yaml"],
            cmd = "printf 'apiVersion: v1\\nkind: ConfigMap\\nmetadata:\\n  name: e2e-scenario\\n  namespace: default\\ndata:\\n  scenario: %s\\n' " + scenario + " > $@",
        )
        manifests[":" + name + "_scenario_yaml"] = scenario + ".yaml"
    images = dict(COMMON_IMAGES)
    images.update(extra_images)
    files = dict(COMMON_FILES)
    files.update(extra_files)
    firecracker_k3s_test(
        name = name,
        scenario = scenario,
        test_binary = test_binary,
        extra_images = images,
        extra_files = files,
        extra_execs = extra_execs,
        extra_manifests = manifests,
        timeout_seconds = timeout_seconds,
        vcpus = vcpus,
        mem_mib = mem_mib,
        size_mb = size_mb,
        **kwargs
    )
