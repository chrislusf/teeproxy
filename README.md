tee-proxy
=========

Why you may need this?
----------------------
You may have production servers running, but you need to upgrade to a new system. You want to run A/B test on both old and new systems to confirm the new system can handle the production load, and want to see whether the new system can run in shadow mode continuously without any issue.

How it works?
-------------
tee-proxy is a reverse proxy. For each incoming request, it clone the request into 2 requests, forward them to 2 servers. The results from server a is returned as usual, but the results from server b is ignored.

tee-proxy handles GET, POST, and all other http methods.

Usage
-------------
 ./teeProxy -l :8888 -a localhost:9000 -b localhost:9001

 "-l" speicifies the listening port. "-a" and "-b" are meant for system A and B. The B system can be taken down or started up without causing any issue to the tee-proxy.
