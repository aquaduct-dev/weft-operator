load("@rules_go//go:def.bzl", "go_context")

def _controller_gen_impl(ctx):
    go = go_context(ctx)
    args = ctx.actions.args()
    args.add_all(ctx.attr.args)

    # Environment with PATH pointing to Go SDK bin
    env = {
        "GOCACHE": "/tmp/gocache",
        "GOPATH": "/tmp/gopath",
        "GOMODCACHE": "/tmp/gomodcache",
        "GOTOOLCHAIN": "local",
        "GOFLAGS": "-mod=readonly",
    }

    # Collect all SDK files
    sdk_files = depset(transitive = [
        go.sdk.srcs,
        go.sdk.libs,
        go.sdk.headers,
        go.sdk.tools,
    ])

    tool = ctx.executable._tool

    dir_outs = []
    for d in ctx.attr.dirs:
        dir_outs.append(ctx.actions.declare_directory(d))

    # Space separated list of output paths
    output_paths = " ".join([f.path for f in ctx.outputs.outs] + [f.path for f in dir_outs])

    ctx.actions.run_shell(
        outputs = ctx.outputs.outs + dir_outs,
        inputs = depset(ctx.files.srcs + [go.go], transitive = [sdk_files]),
        tools = [tool],
        command = """
            export GOCACHE=$(mktemp -d)
            export GOPATH=$(mktemp -d)
            export GOMODCACHE=$(mktemp -d)
            
            export PATH=$PWD/{go_bin_dir}:$PATH
            
            # Run controller-gen
            echo {tool} "$@"
            {tool} "$@"
            RESULT=$?
            
            if [ $RESULT -ne 0 ]; then
                echo "controller-gen failed with exit code $RESULT"
                exit $RESULT
            fi
            
            # Move generated files to declared outputs
            outputs="{output_paths}"
            for out in $outputs; do
                base=$(basename "$out")
                # Find the generated file. We assume it's in the current directory or subdirectories.
                found=$(find . -name "$base" | head -n 1)
                if [ -n "$found" ]; then
                    # echo "Moving $found to $out"
                    if [ -d "$found" ]; then
                        for f in $(ls $found); do
                            cp "$found/$f" "$out/$f"
                        done
                    
                    else
                        cp -r "$found" "$out"
                    fi
                else
                    echo "Error: Expected output $base not generated."
                    echo "Available files:"
                    find . -name "*.go" -o -name "*.yaml"
                    exit 1
                fi
            done
        """.format(
            tool = tool.path,
            output_paths = output_paths,
            go_bin_dir = go.go.dirname,
        ),
        arguments = [args],
        mnemonic = "ControllerGen",
        progress_message = "Running controller-gen %s" % ctx.label,
        env = env,
    )

    return [DefaultInfo(files = depset(ctx.outputs.outs+dir_outs))]

controller_gen_rule = rule(
    implementation = _controller_gen_impl,
    attrs = {
        "srcs": attr.label_list(allow_files = True),
        "outs": attr.output_list(mandatory = True),
        "dirs": attr.string_list(),
        "args": attr.string_list(),
        "_tool": attr.label(
            default = Label("//tools/controller-gen:controller-gen"),
            executable = True,
            cfg = "exec",
        ),
        "_go_context_data": attr.label(
            default = Label("@rules_go//:go_context_data"),
        ),
    },
    toolchains = ["@rules_go//go:toolchain"],
)

def controller_gen(
        name,
        srcs,
        crd_outs = [],
        webhook_outs = [],
        override_paths = [],
        deepcopy_args = ["object:headerFile=/dev/null"],
        rbac_args = ["rbac:roleName=manager-role"],
        crd_args = ["crd"],
        webhook_args = ["webhook"],
        **kwargs):
    # Calculate paths arg
    paths = set()
    if override_paths == []:
        for src in srcs:
            pkg = native.package_relative_label(src).package
            if pkg != "" and pkg != "tools/controller-gen":
                paths.add("./" + pkg)
    else:
        paths = override_paths
    paths_arg = ["paths={" + ",".join(paths) + "}"]

    # Helper to create rule
    def create_rule(suffix,  specific_args,outs=[], dirs=[]):
        # Output dir args
        # We add output configuration to ensure generated files are found easily in the root of execution
        extra_args = []
        if suffix == "rbac":
            extra_args.append("output:rbac:artifacts:config=.")
        elif suffix == "crds":
            extra_args.append("output:crd:dir=./crds")
        elif suffix == "webhook":
            extra_args.append("output:webhook:dir=.")

        controller_gen_rule(
            name = name + "." + suffix,
            srcs = srcs + [native.repo_name() + "//:go.mod", native.repo_name() + "//:go.sum"],
            outs = outs,
            dirs = dirs,
            args = specific_args + paths_arg + extra_args,
            **kwargs
        )

    create_rule("deepcopy", deepcopy_args, outs=["zz_generated.deepcopy.go"])
    create_rule("rbac", rbac_args, outs=["role.yaml"])
    create_rule("crds", crd_args, dirs=["crds"])

    if webhook_outs:
        create_rule("webhook", webhook_outs, webhook_args)
