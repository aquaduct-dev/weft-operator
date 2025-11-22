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
        "PATH": go.sdk_root.path + "/bin:/bin:/usr/bin",
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
        inputs = depset(ctx.files.srcs, transitive = [sdk_files]),
        tools = [tool],
        command = """
            export GOCACHE=$(mktemp -d)
            export GOPATH=$(mktemp -d)
            export GOMODCACHE=$(mktemp -d)
            export PATH=$PATH:{go_bin}
            
            # Run controller-gen
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
                # We look specifically in api/v1alpha1 first as that's where we expect them.
                found=$(find . -name "$base" -type f | head -n 1)
                
                if [ -n "$found" ]; then
                    echo "Moving $found to $out"
                    cp "$found" "$out"
                else
                    echo "Error: Expected output $base not generated."
                    echo "Available files:"
                    find . -name "*.go" -o -name "*.yaml"
                    exit 1
                fi
            done
        """.format(
            go_bin = go.sdk_root.path + "/bin",
            tool = tool.path,
            output_paths = output_paths,
        ),
        arguments = [args],
        mnemonic = "ControllerGen",
        progress_message = "Running controller-gen %s" % ctx.label,
        env = env,
    )

    return [DefaultInfo(files = depset(ctx.outputs.outs))]

controller_gen = rule(
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