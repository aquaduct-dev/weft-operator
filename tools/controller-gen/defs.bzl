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
    
    # Space separated list of output paths
    output_paths = " ".join([f.path for f in ctx.outputs.outs])

    ctx.actions.run_shell(
        outputs = ctx.outputs.outs,
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
                found=$(find . -name "$base" -type f | head -n 1)
                
                if [ -n "$found" ]; then
                    # echo "Moving $found to $out"
                    cp "$found" "$out"
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

    return [DefaultInfo(files = depset(ctx.outputs.outs))]

controller_gen_rule = rule(
    implementation = _controller_gen_impl,
    attrs = {
        "srcs": attr.label_list(allow_files = True),
        "outs": attr.output_list(mandatory = True),
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

def controller_gen(name, srcs, 
                   deepcopy_out = None,
                   rbac_outs = [],
                   crd_outs = [],
                   webhook_outs = [],
                   paths = [],
                   deepcopy_args = ["object:headerFile=/dev/null"],
                   rbac_args = ["rbac:roleName=manager-role"],
                   crd_args = ["crd"],
                   webhook_args = ["webhook"],
                   **kwargs):
    
    # Calculate paths arg
    paths_arg = ["paths={"+",".join(paths)+"}"]
    has_paths = len(paths) > 0
    
    if not has_paths:
        paths_arg = ["paths=./" + native.package_name()]
        
    final_paths_arg = paths_arg # This is either user's paths or default paths
    
    # Helper to create rule
    def create_rule(suffix, outs, specific_args):
        if not outs:
            return
        
        # Output dir args
        # We add output configuration to ensure generated files are found easily in the root of execution
        extra_args = []
        if suffix == "rbac":
            extra_args.append("output:rbac:artifacts:config=.")
        elif suffix == "crds":
            extra_args.append("output:crd:dir=.")
        elif suffix == "webhook":
            extra_args.append("output:webhook:dir=.")
            
        controller_gen_rule(
            name = name + "." + suffix,
            srcs = srcs,
            outs = outs,
            args = specific_args + final_paths_arg + extra_args,
            **kwargs
        )

    if deepcopy_out:
        create_rule("deepcopy", [deepcopy_out], deepcopy_args)
        
    if rbac_outs:
        create_rule("rbac", rbac_outs, rbac_args)
        
    if crd_outs:
        create_rule("crds", crd_outs, crd_args)
        
    if webhook_outs:
        create_rule("webhook", webhook_outs, webhook_args)