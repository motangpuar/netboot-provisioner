#!/bin/bash

while [[ $# -gt 0 ]]; do
  case "$1" in
    -v|--verbose) VERBOSE=1 ;;
    -n|--name) VM="$2"; shift ;;
    -b|--bridge) BRIDGE="$2"; shift ;;
    -m|--mac) MAC="$2"; shift ;;
    --) shift; break ;;
    *) echo "Unknown: $1"; exit 1 ;;
  esac
  shift
done

VM="${VM:-unnamed}"
PORT="${PORT:-8080}"
BRIDGE="${BRIDGE:-tinkbr0}"
MAC="${MAC:-52:54:00:e2:47:eb}"

# 1. Generate XML (VM is NOT created/started)
sudo virt-install \
  --name "$VM" \
  --memory 8192 \
  --vcpus 2 \
  --disk path=/var/lib/libvirt/images/${VM}.qcow2,size=20,format=qcow2 \
  --network bridge=${BRIDGE},model=virtio,mac=${MAC} \
  --boot uefi,network \
  --graphics vnc,listen=0.0.0.0 \
  --os-variant debian12 \
  --print-xml > /tmp/vm.xml

# 2. Remove the existing firmware, loader, and nvram lines
#    - Use patterns that allow spaces anywhere
#    - Remove <firmware>...</firmware> block (including its children)
#    - Remove <loader .../> line
#    - Remove <nvram .../> line
sudo sed -i \
  -e '/<firmware>/,/<\/firmware>/d' \
  -e '/<loader /d' \
  -e '/<nvram /d' \
  /tmp/vm.xml

# 3. Insert the new firmware block right after the <type> line
#    - We use a newline pattern to match the end of the <type> line
sudo sed -i "/<type.*>hvm<\/type>/a \  <firmware>\n    <feature enabled=\"no\" name=\"enrolled-keys\"/>\n    <feature enabled=\"no\" name=\"secure-boot\"/>\n  </firmware>\n  <loader readonly=\"yes\" type=\"pflash\" format=\"raw\">/usr/share/edk2/ovmf/OVMF_CODE.fd</loader>\n  <nvram template=\"/usr/share/edk2/ovmf/OVMF_VARS.fd\" templateFormat=\"raw\" format=\"raw\">/var/lib/libvirt/qemu/nvram/${VM}_VARS.fd</nvram>" /tmp/vm.xml

# 4. Define the VM from the edited XML
sudo virsh define /tmp/vm.xml

# 5. Start the VM
sudo virsh start "$VM"

# Cleanup
rm /tmp/vm.xml
