# Real packet capture testdata.

Captured by using a browser to interact with an `nginx` docker image running
locally.

- `simple_http.pcap`:
    - `GET /`
    - `200 OK` response with `simple_http_response_body_1` as the body
- `simple_http_two.pcap`:
    - The request and response from `simple_http.pcap`
    - An extra HTTP request `GET /favicon.ico` reusing the same TCP connection
    - `404 Not Found` response for `favicon` request with
      `simple_http_response_body_2` as the body
- `simple_http_two_with_noise.pcap`
    - Same as `simple_http_two.pcap`
    - With other non-TCP packets as a result of the pcap being collected from
      nginx running in docker.
