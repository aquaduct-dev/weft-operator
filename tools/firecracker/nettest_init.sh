#!/bin/sh
# nettest_init — PID 1 for the firecracker networking self-test (CRITERIA
# "No stub tests" multi-VM amendment). The kernel configures eth0 from the
# `ip=` cmdline (CONFIG_IP_PNP); this init parses its own IP and the peer's
# from /proc/cmdline, pings the peer over the shared bridge, and prints a
# NETOK / NETFAIL marker the orchestrator greps. Two of these VMs on one
# bridge pinging each other prove the multi-VM substrate end to end.
mount -t proc proc /proc 2>/dev/null
mount -t sysfs sysfs /sys 2>/dev/null

CMDLINE="$(cat /proc/cmdline)"
SELF="$(echo "$CMDLINE" | sed -n 's/.*[ ]ip=\([0-9.]*\)::.*/\1/p')"
PEER="$(echo "$CMDLINE" | sed -n 's/.*ofd\.peer=\([0-9.]*\).*/\1/p')"

# eth0 is brought up + addressed by the kernel ip= autoconfig; make sure
# loopback is up too.
ip link set lo up 2>/dev/null

echo "NETBOOT self=$SELF peer=$PEER"
echo "NETBOOT ifaces: $(ip -o -4 addr show 2>/dev/null | tr '\n' ';')"

ok=0
i=0
while [ "$i" -lt 15 ]; do
    if ping -c1 -W1 "$PEER" >/dev/null 2>&1; then
        ok=1
        break
    fi
    i=$((i + 1))
    sleep 1
done

if [ "$ok" = 1 ]; then
    echo "NETOK ${SELF}->${PEER}"
else
    echo "NETFAIL ${SELF}->${PEER}"
fi

poweroff -f
