Simple QUIC holepunch demonstration

Usage:
```shell
// On computer 1
> go build ./client
> client.exe -msg "Hello from client 1"
Local address: ......
Your address is: <public IP:port>
Enter remote peer address (with port): <public IP:port of computer 2>

// On computer 2
> client.exe -msg "Hello from client 2"
Local address: ......
Your address is: <public IP:port>
Enter remote peer address (with port): <public IP:port of computer 1>
```
The peers should exchange messages. You can then add more peers or simply disconnect.

I have tested this application using two computers: one connected to basic consumer internet (ATT), which is using a Normal/Restricted Cone NAT (internal port = external port). This NAT is basic enough that even outdated methods of NAT traversal will still work.
The second computer is connected to a LTE hotspot (T-Mobile), which is most likely using carrier grade NAT, that is at least Restricted NAT and the port numbers are not 1:1 mapping for internal/external ports (it is not a symmetric NAT).

Note that each computer needs to enter the others address within ~10-15 seconds of each other in order to successfully holepunch.
In practice, it is better to use some sort of rendezvous point, but that requires a third party server, so I kept my implementation simple.

There was a tool called `pwnat` (http://samy.pl/pwnat) that was able to do this without even a rendezvous or any kind of client synchronization, but that no longer works.
`chownat`, made by the same author, does not work either anymore, rendering this approach impossible without some other way to signal peers.
