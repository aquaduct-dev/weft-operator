"""Bazel macros for building and testing Firecracker microVMs.

Public API:

  firecracker_initramfs(name, init, files = {}, execs = {}, busybox = ...)
      Pack a cpio+gzip initramfs. `init` is the script installed at /init
      (PID 1). `files` and `execs` are {label: dst_path} maps for regular
      and executable files respectively; ELF entries in `execs` are
      checked for static linkage at build time. Busybox applets are
      auto-symlinked into /bin (and a few into /sbin).

  firecracker_rootfs(name, size_mb, label, files = {}, execs = {})
      Build an ext4 disk image via `mkfs.ext4 -d`. Output: <name>.ext4.
      Requires e2fsprogs >=1.43 on the host (Debian 10+).

  firecracker_microvm(name, kernel, initrd | rootfs, kernel_cmdline,
                      vcpus, mem_mib)
      Emit a runnable sh_binary that boots the VM under host firecracker.
      Exactly one of `initrd` or `rootfs` must be set.

  firecracker_test(name, microvm, expect_markers, unexpected_markers,
                   timeout_seconds)
      sh_test that boots a `firecracker_microvm` for `timeout_seconds`,
      tees serial-console output, then asserts every `expect_markers`
      string is present and every `unexpected_markers` string is absent.
      Tagged `requires-kvm` and `no-cache`. Run with `--config=kvm`
      (defined in //.bazelrc) to bind-mount /dev/kvm into the sandbox.
      The VM is killed as soon as every expect_marker has appeared, so
      typical tests finish in a few seconds regardless of timeout_seconds.

  firecracker_container(name, image, size_mb, kernel, extra_env, extra_args,
                        kernel_cmdline, vcpus, mem_mib, busybox)
      Boot a single OCI image (`rules_oci` oci_image label) inside a
      microVM. The image's flattened filesystem becomes the rootfs and
      a generated /sbin/init mounts /proc /sys /dev, exports the image's
      Env (plus `extra_env`), cd's to WorkingDir, then runs the image's
      Entrypoint+Cmd (plus `extra_args`). On exit, init poweroffs the
      VM cleanly. The macro expands to a runnable `firecracker_microvm`
      named `<name>`. Useful for booting kubernetes node images or
      single-container workloads under VM isolation.

  firecracker_k3s_test(name, test_binary, scenario, extra_manifests,
                       extra_images, extra_files, extra_execs,
                       timeout_seconds, vcpus, mem_mib, size_mb)
      Boot a single-node k3s server inside a firecracker microVM and
      run a statically-linked guest-side `test_binary` against it. K3s
      is installed directly onto the rootfs (the static k3s binary at
      /usr/local/bin/k3s; runs as PID-1's child, no container nesting).
      The test binary reads its kubeconfig from
      /etc/rancher/k3s/k3s.yaml, writes a lib/clusterharness Report JSON
      to its --report-json path, and the host launcher (a `sh_test`)
      scrapes that JSON out of the serial console between
      `===REPORT_BEGIN===`/`===REPORT_END===` markers. Pass/fail is
      decided by every pass_condition being passed=true.
"""

load("@rules_shell//shell:sh_binary.bzl", "sh_binary")
load("@rules_shell//shell:sh_test.bzl", "sh_test")

_DEFAULT_BUSYBOX = "//tools/firecracker:busybox_static"
_DEFAULT_KERNEL = "//tools/firecracker:vmlinux"
_DEFAULT_K3S = "//tools/firecracker:k3s_binary"
_MKINITRAMFS = "//tools/firecracker:mkinitramfs"
_MKROOTFS = "//tools/firecracker:mkrootfs"
_MKCONTAINERFS = "//tools/firecracker:mkcontainerfs"
_MKK3SROOTFS = "//tools/firecracker:mkk3srootfs"
_GEN_TEST = "//tools/firecracker:gen_test"
_GEN_K3S_TEST = "//tools/firecracker:gen_k3s_test"
_K3S_INIT = "//tools/firecracker:k3s_init.sh"
_RUNNER_TPL = "//tools/firecracker:runner.sh.tpl"
_CLUSTER_RUN = "//tools/firecracker:cluster_run.sh"
_CLUSTER_TEST_TPL = "//tools/firecracker:cluster_test.sh.tpl"
_RENUMBER_TEST_TPL = "//tools/firecracker:cluster_renumber_test.sh.tpl"
_INSTALL_TEST_TPL = "//tools/firecracker:cluster_install_test.sh.tpl"

# `random.trust_cpu=on` lets the kernel seed its RNG from RDRAND so
# Go's crypto/rand doesn't block ~60s waiting for entropy. Pinned
# kernel (vmlinux-6.1.102) honors the arg.
_DEFAULT_CMDLINE = "console=ttyS0 reboot=k panic=1 pci=off random.trust_cpu=on"

def _shell_quote(s):
    # Single-quote for sed/shell embedding. Replace ' with '\''.
    return "'" + s.replace("'", "'\\''") + "'"

def _rlocation_path(label):
    # rlocation paths under bzlmod for the root module are _main/<pkg>/<name>.
    # Used to address a sh_binary's launcher script from a sibling sh_test —
    # $(rlocationpath label) would expand to multiple files (the runner script
    # plus the binary itself), so we compute it ourselves at macro-expansion
    # time. Verified by inspecting smoke_vm.runfiles/.
    abs = native.package_relative_label(label)
    if abs.package:
        return "_main/{}/{}".format(abs.package, abs.name)
    return "_main/{}".format(abs.name)

def firecracker_initramfs(
        name,
        init,
        files = {},
        execs = {},
        busybox = _DEFAULT_BUSYBOX,
        **kwargs):
    """Pack a cpio+gzip initramfs from a set of files.

    Args:
      name: target name; output is <name>.cpio.gz.
      init: label installed at /init (PID 1).
      files: {label: dst_path} regular files (0644).
      execs: {label: dst_path} executable files (0755). ELF entries are
        checked for static linkage at build time.
      busybox: label to a statically-linked busybox binary.
      **kwargs: forwarded to the underlying genrule (visibility, tags...).
    """
    srcs = [init, busybox] + list(files.keys()) + list(execs.keys())
    args = [
        "--out '$@'",
        "--busybox $(execpath {})".format(busybox),
        "--init $(execpath {})".format(init),
    ]
    for src, dst in files.items():
        args.append("--file \"$(execpath {})={}\"".format(src, dst))
    for src, dst in execs.items():
        args.append("--exec \"$(execpath {})={}\"".format(src, dst))

    native.genrule(
        name = name,
        srcs = srcs,
        outs = [name + ".cpio.gz"],
        cmd = "$(execpath {tool}) {args}".format(
            tool = _MKINITRAMFS,
            args = " ".join(args),
        ),
        tools = [_MKINITRAMFS],
        # Host busybox + cpio + gzip; not hermetic.
        local = True,
        **kwargs
    )

def firecracker_rootfs(
        name,
        size_mb,
        label,
        files = {},
        execs = {},
        **kwargs):
    """Build an ext4 disk image with `mkfs.ext4 -d`.

    Args:
      name: target name; output is <name>.ext4.
      size_mb: image size in megabytes.
      label: filesystem label (max 16 chars).
      files: {label: dst_path} regular files (0644).
      execs: {label: dst_path} executable files (0755).
      **kwargs: forwarded to the underlying genrule (visibility, tags...).
    """
    srcs = list(files.keys()) + list(execs.keys())
    args = [
        "--out '$@'",
        "--size-mb {}".format(size_mb),
        "--label {}".format(label),
    ]
    for src, dst in files.items():
        args.append("--file \"$(execpath {})={}\"".format(src, dst))
    for src, dst in execs.items():
        args.append("--exec \"$(execpath {})={}\"".format(src, dst))

    native.genrule(
        name = name,
        srcs = srcs,
        outs = [name + ".ext4"],
        cmd = "$(execpath {tool}) {args}".format(
            tool = _MKROOTFS,
            args = " ".join(args),
        ),
        tools = [_MKROOTFS],
        # Host mkfs.ext4 (e2fsprogs).
        local = True,
        **kwargs
    )

def firecracker_microvm(
        name,
        kernel,
        initrd = None,
        rootfs = None,
        extra_drive = None,
        kernel_cmdline = _DEFAULT_CMDLINE,
        vcpus = 1,
        mem_mib = 256,
        visibility = None,
        tags = None,
        **kwargs):
    """Emit a runnable sh_binary that boots a firecracker microVM.

    Args:
      name: target name; sh_binary label.
      kernel: label of an uncompressed kernel ELF.
      initrd: label of a cpio.gz initramfs (mutually exclusive with rootfs).
      rootfs: label of an ext4 disk image (mutually exclusive with initrd).
      extra_drive: optional label of a disk image to attach as a
        writable virtio-block device. The runner copies it into
        $WORK before booting (so the source bazel-bin file stays
        immutable). In initrd-boot mode it enumerates as /dev/vda
        inside the guest. Used by §23.10 to hand the installer a
        blank target disk; set FC_KEEP_WORK=1 at runtime to preserve
        $WORK so callers can inspect the written-to image afterward.
      kernel_cmdline: boot args passed to the kernel.
      vcpus: vCPU count.
      mem_mib: guest memory in MiB.
      visibility: target visibility.
      tags: target tags. The macro auto-adds `manual`.
      **kwargs: forwarded to sh_binary.
    """
    if (initrd == None) == (rootfs == None):
        fail("firecracker_microvm: exactly one of `initrd` or `rootfs` required")

    boot_label = initrd if initrd != None else rootfs
    boot_kind = "initrd" if initrd != None else "rootfs"
    runner_src = "_" + name + "__runner.sh"

    srcs = [kernel, boot_label, _RUNNER_TPL]
    data = [kernel, boot_label]
    if extra_drive != None:
        srcs.append(extra_drive)
        data.append(extra_drive)
        extra_drive_rloc_expr = "$(rlocationpath {})".format(extra_drive)
    else:
        extra_drive_rloc_expr = ""

    native.genrule(
        name = name + "__gen_runner",
        srcs = srcs,
        outs = [runner_src],
        cmd = (
            "sed " +
            "-e 's|@@KERNEL_RLOC@@|$(rlocationpath {kernel})|g' " +
            "-e 's|@@BOOT_RLOC@@|$(rlocationpath {boot})|g' " +
            "-e 's|@@BOOT_KIND@@|{kind}|g' " +
            "-e 's|@@EXTRA_DRIVE_RLOC@@|{extra}|g' " +
            "-e 's|@@KERNEL_CMDLINE@@|{cmdline}|g' " +
            "-e 's|@@VCPUS@@|{vcpus}|g' " +
            "-e 's|@@MEM_MIB@@|{mem_mib}|g' " +
            "$(execpath {tpl}) > $@ && chmod +x $@"
        ).format(
            kernel = kernel,
            boot = boot_label,
            kind = boot_kind,
            extra = extra_drive_rloc_expr,
            cmdline = kernel_cmdline,
            vcpus = vcpus,
            mem_mib = mem_mib,
            tpl = _RUNNER_TPL,
        ),
        tags = ["manual"],
    )

    sh_binary(
        name = name,
        srcs = [runner_src],
        data = data,
        deps = ["@bazel_tools//tools/bash/runfiles"],
        visibility = visibility,
        tags = (tags or []) + ["manual"],
        **kwargs
    )

def firecracker_test(
        name,
        microvm,
        expect_markers = [],
        unexpected_markers = [],
        timeout_seconds = 30,
        size = "medium",
        visibility = None,
        tags = None,
        **kwargs):
    """sh_test that boots a microvm and asserts on serial-console output.

    Args:
      name: target name; sh_test label.
      microvm: label of a firecracker_microvm.
      expect_markers: substrings that must appear in the boot log
        (fixed-string `grep -F`).
      unexpected_markers: substrings that must NOT appear in the boot log.
      timeout_seconds: how long the VM is allowed to run before the host
        kills it (kill is treated as success — assertions run on whatever
        output was captured).
      size: sh_test size attr.
      visibility: target visibility.
      tags: target tags. The macro auto-adds `requires-kvm` and `no-cache`.
      **kwargs: forwarded to sh_test.
    """
    test_src = "_" + name + "__test.sh"

    args = [
        "--out '$@'",
        "--runner-rloc {}".format(_rlocation_path(microvm)),
        "--timeout {}".format(timeout_seconds),
    ]
    for m in expect_markers:
        args.append("--expect {}".format(_shell_quote(m)))
    for m in unexpected_markers:
        args.append("--unexpect {}".format(_shell_quote(m)))

    native.genrule(
        name = name + "__gen_test",
        srcs = [microvm],
        outs = [test_src],
        cmd = "$(execpath {tool}) {args}".format(
            tool = _GEN_TEST,
            args = " ".join(args),
        ),
        tools = [_GEN_TEST],
        tags = ["manual"],
    )

    sh_test(
        name = name,
        srcs = [test_src],
        data = [microvm],
        deps = ["@bazel_tools//tools/bash/runfiles"],
        size = size,
        visibility = visibility,
        # `requires-kvm` lets `--test_tag_filters=-requires-kvm` skip on
        # KVM-less hosts. `no-cache` because boot-time markers are
        # timing-sensitive and shouldn't be cached.
        tags = (tags or []) + ["requires-kvm", "no-cache"],
        **kwargs
    )

def firecracker_container(
        name,
        image,
        size_mb = 512,
        kernel = _DEFAULT_KERNEL,
        extra_env = {},
        extra_args = [],
        # `init=/sbin/init` makes the kernel call our generated init
        # instead of guessing — some OCI images already ship /sbin/init
        # (e.g. distros with sysvinit) and we want ours to win.
        kernel_cmdline = _DEFAULT_CMDLINE + " init=/sbin/init",
        vcpus = 1,
        mem_mib = 512,
        busybox = _DEFAULT_BUSYBOX,
        visibility = None,
        tags = None,
        **kwargs):
    """Boot a single OCI image inside a firecracker microVM.

    Expands to a runnable `firecracker_microvm` whose rootfs is the
    image's flattened filesystem plus a generated /sbin/init that mounts
    /proc /sys /dev /tmp /run, exports the image Env (plus `extra_env`),
    cd's to WorkingDir, runs Entrypoint+Cmd (plus `extra_args`), then
    poweroffs the VM when the container exits.

    Args:
      name: target name; the runnable microvm sh_binary.
      image: label of an `oci_image` (or any rule emitting an OCI image
        layout directory: index.json + blobs/sha256/...).
      size_mb: rootfs ext4 size in megabytes. Must be larger than the
        flattened image — fail-loud at mkfs time otherwise.
      kernel: label of an uncompressed kernel ELF. Defaults to the
        pinned firecracker vmlinux.
      extra_env: {KEY: VAL} env vars appended to the image's Env. Same-
        named keys override the image's value (later assignment wins).
      extra_args: list of extra args appended after the image's
        Entrypoint+Cmd.
      kernel_cmdline: boot args. Defaults include `init=/sbin/init` so
        the kernel uses our generated init even when the image ships
        its own /sbin/init.
      vcpus: vCPU count.
      mem_mib: guest memory in MiB.
      busybox: statically-linked busybox label; copied to /sbin/busybox
        inside the image and used by /sbin/init for mounts and poweroff.
      visibility: target visibility, forwarded to the microvm.
      tags: target tags, forwarded to the microvm. The macro auto-adds
        `manual` on the intermediate rootfs genrule.
      **kwargs: forwarded to the firecracker_microvm.
    """
    rootfs_name = "_" + name + "__rootfs"

    args = [
        "--out '$@'",
        "--image-dir '$(execpath {})'".format(image),
        "--busybox '$(execpath {})'".format(busybox),
        "--size-mb {}".format(size_mb),
        "--label container",
    ]
    for k, v in extra_env.items():
        args.append("--extra-env {}".format(_shell_quote("{}={}".format(k, v))))
    for a in extra_args:
        args.append("--extra-arg {}".format(_shell_quote(a)))

    native.genrule(
        name = rootfs_name,
        srcs = [image, busybox],
        outs = [rootfs_name + ".ext4"],
        cmd = "$(execpath {tool}) {args}".format(
            tool = _MKCONTAINERFS,
            args = " ".join(args),
        ),
        tools = [_MKCONTAINERFS],
        # Host mkfs.ext4 + python3 + tar.
        local = True,
        tags = ["manual"],
    )

    firecracker_microvm(
        name = name,
        kernel = kernel,
        rootfs = ":" + rootfs_name,
        kernel_cmdline = kernel_cmdline,
        vcpus = vcpus,
        mem_mib = mem_mib,
        visibility = visibility,
        tags = tags,
        **kwargs
    )

def firecracker_k3s_test(
        name,
        test_binary,
        scenario,
        extra_manifests = {},
        extra_images = {},
        extra_files = {},
        extra_execs = {},
        timeout_seconds = 300,
        vcpus = 2,
        mem_mib = 2048,
        size_mb = 2048,
        kernel = _DEFAULT_KERNEL,
        kernel_cmdline = _DEFAULT_CMDLINE,
        busybox = _DEFAULT_BUSYBOX,
        k3s = _DEFAULT_K3S,
        init = _K3S_INIT,
        size = "enormous",
        visibility = None,
        tags = None,
        **kwargs):
    """Boot a single-node k3s cluster in a firecracker microVM and run a guest-side test binary against it.

    The test binary is responsible for:
      1. Reading the kubeconfig at /etc/rancher/k3s/k3s.yaml.
      2. Writing a lib/clusterharness Report JSON to --report-json.
      3. Returning exit 0 iff every pass_condition has passed=true.

    The host launcher boots the VM, captures the serial console between
    `===REPORT_BEGIN===`/`===REPORT_END===` markers, writes that to the
    test's $SCENARIO_REPORT_PATH (or test-undeclared-outputs if unset),
    and asserts the report's pass_conditions are all green.

    Args:
      name: target name; sh_test label.
      test_binary: a STATICALLY-LINKED go_binary that takes
        `--scenario=PATH --report-json=PATH --kubeconfig=PATH --namespace=STR`.
        rules_go produces static binaries by default for cgo-free Go code.
      scenario: scenario basename (the test binary will be invoked with
        --scenario=/etc/manifests/<scenario>.yaml). Also baked into the
        rootfs at /etc/scenario.
      extra_manifests: {label: basename.yaml} YAMLs applied via
        `kubectl apply` after k3s is Ready, before the test runs. Useful
        for installing CRDs and fixture CRs.
      extra_images: {label: basename.tar} OCI image tarballs preloaded
        into k3s' embedded containerd (so no external pull is needed).
      extra_files: {label: dst-abs-path} regular files (0644) layered
        into the rootfs (e.g. additional Secrets, kubeconfigs, etc.).
      extra_execs: {label: dst-abs-path} executable files (0755).
      timeout_seconds: hard cap on VM wall time. K3s cold-start is
        ~30-60s on this host; budget 300s by default.
      vcpus: vCPU count. 2 is the minimum for a snappy k3s.
      mem_mib: guest memory. 2048 MiB covers k3s server + a couple of
        small test pods comfortably.
      size_mb: rootfs ext4 size. 2 GiB leaves headroom for k3s' runtime
        unpack (~150 MiB), pulled/imported images, and emptyDir tmpfs.
      kernel: firecracker boot kernel.
      kernel_cmdline: kernel command line.
      busybox: static busybox label.
      k3s: static k3s label.
      init: /sbin/init script label.
      size: sh_test size attr; `enormous` matches typical k3s cold-start.
      visibility: target visibility.
      tags: extra tags. `requires-kvm` and `no-cache` auto-added.
      **kwargs: forwarded to sh_test.
    """
    rootfs_name = "_" + name + "__rootfs"
    microvm_name = "_" + name + "__microvm"
    test_src = "_" + name + "__test.sh"

    # Resolve the test_binary label to a basename for /usr/local/bin/.
    abs = native.package_relative_label(test_binary)
    test_basename = abs.name

    args = [
        "--out '$@'",
        "--busybox '$(execpath {})'".format(busybox),
        "--k3s '$(execpath {})'".format(k3s),
        "--init '$(execpath {})'".format(init),
        "--test-binary '$(execpath {})'".format(test_binary),
        "--test-binary-name {}".format(_shell_quote(test_basename)),
        "--scenario {}".format(_shell_quote(scenario)),
        "--size-mb {}".format(size_mb),
        "--label k3s",
    ]
    srcs = [busybox, k3s, init, test_binary]
    for label, dst in extra_manifests.items():
        args.append("--manifest \"$(execpath {})={}\"".format(label, dst))
        srcs.append(label)
    for label, dst in extra_images.items():
        args.append("--image \"$(execpath {})={}\"".format(label, dst))
        srcs.append(label)
    for label, dst in extra_files.items():
        args.append("--file \"$(execpath {})={}\"".format(label, dst))
        srcs.append(label)
    for label, dst in extra_execs.items():
        args.append("--exec \"$(execpath {})={}\"".format(label, dst))
        srcs.append(label)

    native.genrule(
        name = rootfs_name,
        srcs = srcs,
        outs = [rootfs_name + ".ext4"],
        cmd = "$(execpath {tool}) {args}".format(
            tool = _MKK3SROOTFS,
            args = " ".join(args),
        ),
        tools = [_MKK3SROOTFS],
        local = True,
        tags = ["manual"],
    )

    firecracker_microvm(
        name = microvm_name,
        kernel = kernel,
        rootfs = ":" + rootfs_name,
        kernel_cmdline = kernel_cmdline,
        vcpus = vcpus,
        mem_mib = mem_mib,
        tags = ["manual"],
    )

    gen_args = [
        "--out '$@'",
        "--runner-rloc {}".format(_rlocation_path(":" + microvm_name)),
        "--timeout {}".format(timeout_seconds),
    ]

    native.genrule(
        name = name + "__gen_test",
        srcs = [":" + microvm_name],
        outs = [test_src],
        cmd = "$(execpath {tool}) {args}".format(
            tool = _GEN_K3S_TEST,
            args = " ".join(gen_args),
        ),
        tools = [_GEN_K3S_TEST],
        tags = ["manual"],
    )

    sh_test(
        name = name,
        srcs = [test_src],
        data = [":" + microvm_name],
        deps = ["@bazel_tools//tools/bash/runfiles"],
        size = size,
        visibility = visibility,
        tags = (tags or []) + ["requires-kvm", "no-cache"],
        **kwargs
    )

def firecracker_cluster_test(
        name,
        vm,
        nodes,
        expect_marker,
        leader_idx,
        subnet_gw = "10.211.55.1/24",
        timeout_seconds = 360,
        size = "enormous",
        visibility = None,
        tags = None,
        **kwargs):
    """Boot N real firecracker microVMs of `vm` on a shared bridge and assert a marker.

    The REAL multi-VM rig the CRITERIA "No stub tests" rule requires: it runs
    `cluster_run.sh` (host bridge + one user-owned TAP per node) to boot every
    node, then greps the leader node's serial log for `expect_marker`. Runs as
    a `local` (unsandboxed) test so it has the host network namespace +
    passwordless sudo for TAP/bridge setup and /dev/kvm; it SKIPs (exit 0) when
    either is unavailable.

    Args:
      vm: a firecracker_microvm label (its rootfs runs the real workload).
      nodes: list of "IDX,IP,MAC,EXTRA_CMDLINE" spec strings (one per VM).
      expect_marker: substring that must appear in the leader's log to pass.
      leader_idx: the IDX whose log carries expect_marker.
      subnet_gw: bridge gateway IP/CIDR.
      timeout_seconds: per-VM hard cap (FC_TIMEOUT).
    """
    test_src = "_" + name + "__cluster_test.sh"
    nodes_joined = ";".join(nodes)

    # vm is an sh_binary (multi-output), so rlocationpath won't resolve it.
    # Compute its runfiles path from the label: main-repo executables live
    # at _main/<package>/<name> under the runfiles root.
    vm_label = native.package_relative_label(vm)
    vm_rloc = "_main/" + vm_label.package + "/" + vm_label.name

    native.genrule(
        name = name + "__gen",
        srcs = [_CLUSTER_TEST_TPL, _CLUSTER_RUN],
        outs = [test_src],
        cmd = (
            "sed " +
            "-e 's|@@RUNNER_RLOC@@|{vm_rloc}|g' " +
            "-e 's|@@CLUSTER_RUN_RLOC@@|$(rlocationpath {crun})|g' " +
            "-e 's|@@NODES@@|{nodes}|g' " +
            "-e 's|@@SUBNET_GW@@|{gw}|g' " +
            "-e 's|@@TIMEOUT@@|{timeout}|g' " +
            "-e 's|@@LEADER_IDX@@|{leader}|g' " +
            "-e 's|@@EXPECT_MARKER@@|{marker}|g' " +
            "$(execpath {tpl}) > $@ && chmod +x $@"
        ).format(
            vm_rloc = vm_rloc,
            crun = _CLUSTER_RUN,
            nodes = nodes_joined,
            gw = subnet_gw,
            timeout = timeout_seconds,
            leader = leader_idx,
            marker = expect_marker,
            tpl = _CLUSTER_TEST_TPL,
        ),
        tools = [],
        tags = ["manual"],
    )

    sh_test(
        name = name,
        srcs = [test_src],
        data = [vm, _CLUSTER_RUN],
        deps = ["@bazel_tools//tools/bash/runfiles"],
        size = size,
        visibility = visibility,
        # local + no-sandbox: needs the host net namespace + sudo for TAP/bridge.
        tags = (tags or []) + ["requires-kvm", "no-cache", "local", "no-sandbox", "manual"],
        **kwargs
    )

def firecracker_renumber_test(
        name,
        vm,
        nodes1,
        nodes2,
        gw1,
        gw2,
        expect_marker,
        timeout1 = 240,
        timeout2 = 300,
        size = "enormous",
        visibility = None,
        tags = None,
        **kwargs):
    """Two-phase real multi-VM rig for CRITERIA §3/§4b (IP-renumber heal).

    Boots `vm` nodes on gw1's subnet (they form a cluster + persist identity
    via --persist-dir, then power off), then reboots the SAME nodes (same
    persistent disks) on gw2's subnet and asserts `expect_marker` (e.g.
    OFD_HEAL_OK) appears in a phase-2 serial log. nodes1/nodes2 are the
    per-phase "IDX,IP,MAC,EXTRA" spec lists (same MACs + IDX, different IPs).
    """
    test_src = "_" + name + "__renumber_test.sh"
    vm_label = native.package_relative_label(vm)
    vm_rloc = "_main/" + vm_label.package + "/" + vm_label.name

    native.genrule(
        name = name + "__gen",
        srcs = [_RENUMBER_TEST_TPL, _CLUSTER_RUN],
        outs = [test_src],
        cmd = (
            "sed " +
            "-e 's|@@RUNNER_RLOC@@|{vm_rloc}|g' " +
            "-e 's|@@CLUSTER_RUN_RLOC@@|$(rlocationpath {crun})|g' " +
            "-e 's|@@NODES1@@|{nodes1}|g' " +
            "-e 's|@@NODES2@@|{nodes2}|g' " +
            "-e 's|@@GW1@@|{gw1}|g' -e 's|@@GW2@@|{gw2}|g' " +
            "-e 's|@@TIMEOUT1@@|{t1}|g' -e 's|@@TIMEOUT2@@|{t2}|g' " +
            "-e 's|@@EXPECT_MARKER@@|{marker}|g' " +
            "$(execpath {tpl}) > $@ && chmod +x $@"
        ).format(
            vm_rloc = vm_rloc,
            crun = _CLUSTER_RUN,
            nodes1 = ";".join(nodes1),
            nodes2 = ";".join(nodes2),
            gw1 = gw1,
            gw2 = gw2,
            t1 = timeout1,
            t2 = timeout2,
            marker = expect_marker,
            tpl = _RENUMBER_TEST_TPL,
        ),
        tags = ["manual"],
    )

    sh_test(
        name = name,
        srcs = [test_src],
        data = [vm, _CLUSTER_RUN],
        deps = ["@bazel_tools//tools/bash/runfiles"],
        size = size,
        visibility = visibility,
        tags = (tags or []) + ["requires-kvm", "no-cache", "local", "no-sandbox", "manual"],
        **kwargs
    )

def firecracker_install_cluster_test(
        name,
        vm,
        installer,
        rootfs,
        nodes,
        expect_marker,
        gw = "10.211.55.1/24",
        secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        timeout_seconds = 360,
        size = "enormous",
        visibility = None,
        tags = None,
        **kwargs):
    """Real §28 rig: install each node to a GPT disk via the REAL installer,
    then boot every node from its installed disk (no media) and assert a
    cluster forms. `installer` is the openfactoryd-installer binary label;
    `rootfs` is the bootable cluster rootfs ext4 image label."""
    test_src = "_" + name + "__install_test.sh"
    vm_label = native.package_relative_label(vm)
    vm_rloc = "_main/" + vm_label.package + "/" + vm_label.name
    inst_label = native.package_relative_label(installer)
    inst_rloc = "_main/" + inst_label.package + "/" + inst_label.name + "_/" + inst_label.name
    idxs = " ".join([spec.split(",")[0] for spec in nodes])

    native.genrule(
        name = name + "__gen",
        srcs = [_INSTALL_TEST_TPL, _CLUSTER_RUN, rootfs],
        outs = [test_src],
        cmd = (
            "sed " +
            "-e 's|@@RUNNER_RLOC@@|{vm_rloc}|g' " +
            "-e 's|@@INSTALLER_RLOC@@|{inst_rloc}|g' " +
            "-e 's|@@CLUSTER_RUN_RLOC@@|$(rlocationpath {crun})|g' " +
            "-e 's|@@ROOTFS_RLOC@@|$(rlocationpath {rootfs})|g' " +
            "-e 's|@@NODES@@|{nodes}|g' -e 's|@@IDXS@@|{idxs}|g' " +
            "-e 's|@@GW@@|{gw}|g' -e 's|@@TIMEOUT@@|{tmo}|g' " +
            "-e 's|@@SECRET@@|{secret}|g' -e 's|@@EXPECT_MARKER@@|{marker}|g' " +
            "$(execpath {tpl}) > $@ && chmod +x $@"
        ).format(
            vm_rloc = vm_rloc,
            inst_rloc = inst_rloc,
            crun = _CLUSTER_RUN,
            rootfs = rootfs,
            nodes = ";".join(nodes),
            idxs = idxs,
            gw = gw,
            tmo = timeout_seconds,
            secret = secret,
            marker = expect_marker,
            tpl = _INSTALL_TEST_TPL,
        ),
        tags = ["manual"],
    )

    sh_test(
        name = name,
        srcs = [test_src],
        data = [vm, installer, rootfs, _CLUSTER_RUN],
        deps = ["@bazel_tools//tools/bash/runfiles"],
        size = size,
        visibility = visibility,
        tags = (tags or []) + ["requires-kvm", "no-cache", "local", "no-sandbox", "manual"],
        **kwargs
    )
