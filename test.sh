sudo ip link add eth10 type dummy
sudo ip link set dev eth10 up
sudo ip addr add 192.168.100.199/24 dev eth10


