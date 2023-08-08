# go load balancer

this is a simple load balancer written in go.

as we know, load balancer have different algorithms to balance the load, this load balancer uses the round robin algorithm.

Round Robin gives equal opportnities for workers to perform tasks in turns.

for backend down situation, this load balancer routes traffic only to the healthy backends.
