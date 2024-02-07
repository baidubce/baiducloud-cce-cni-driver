
# allocation IP
POST /v1/ippool/{{poolname}}/alloc?clientToken=ds-hng79d  HTTP/1.1
Host: bcc.bj.baidubce.com
authorization: bce-auth-v1/c3119080364d11e8912505e5e0ae9978/2018-04-02T08:14:25Z/3600/host;x-bce-account;x-bce-client-ip;x-bce-date;x-bce-request-id;x-bce-security-token/6b93bfd5ca2328fcb5560fa57f56253d80a629f6aac9c9cb74adf9a055fceb53
{
    "ipAddressCount": 3
}

# response

HTTP/1.1 200 OK
x-bce-request-id: 7e789a40-adac-414a-8bd4-916d6be61112
Date: Mon, 02 Apr 2019 08:14:25 GMT
Content-Type: application/json;charset=UTF-8
Server: BWS
{
	"ipAddresses": [
		"192.168.1.1",
		"192.168.1.2",
		"192.168.1.3"
	]
}

# ---------
# release IP
POST /v1/ippool/{{poolname}}/release?clientToken=ds-hng79d  HTTP/1.1
Host: bcc.bj.baidubce.com
authorization: bce-auth-v1/c3119080364d11e8912505e5e0ae9978/2018-04-02T08:14:25Z/3600/host;x-bce-account;x-bce-client-ip;x-bce-date;x-bce-request-id;x-bce-security-token/6b93bfd5ca2328fcb5560fa57f56253d80a629f6aac9c9cb74adf9a055fceb53
{
	"ipAddresses": [
		"192.168.1.1",
		"192.168.1.2",
		"192.168.1.3"
	]
}

# response
HTTP/1.1 200 OK
x-bce-request-id: 7e789a40-adac-414a-8bd4-916d6be61112
Date: Mon, 02 Apr 2019 08:14:25 GMT
Content-Type: application/json;charset=UTF-8
Server: BWS


# ---------
# list IPPool
GET /v1/ippool  HTTP/1.1
Host: bcc.bj.baidubce.com
authorization: bce-auth-v1/c3119080364d11e8912505e5e0ae9978/2018-04-02T08:14:25Z/3600/host;x-bce-account;x-bce-client-ip;x-bce-date;x-bce-request-id;x-bce-security-token/6b93bfd5ca2328fcb5560fa57f56253d80a629f6aac9c9cb74adf9a055fceb53

# response
HTTP/1.1 200 OK
x-bce-request-id: 7e789a40-adac-414a-8bd4-916d6be61112
Date: Mon, 02 Apr 2019 08:14:25 GMT
Content-Type: application/json;charset=UTF-8
Server: BWS
{
    "pools":[
        {
            "name":"pool1",
            "ranges":[
                [
                    {
                        "subnet": "172.16.5.0/24"
                    }
                ]
            ]
        }
    ]
}



# ---------
# GET IPPool
GET /v1/ippool/{{poolname}}  HTTP/1.1
Host: bcc.bj.baidubce.com
authorization: bce-auth-v1/c3119080364d11e8912505e5e0ae9978/2018-04-02T08:14:25Z/3600/host;x-bce-account

# response
HTTP/1.1 200 OK
x-bce-request-id: 7e789a40-adac-414a-8bd4-916d6be61112
Date: Mon, 02 Apr 2019 08:14:25 GMT
Content-Type: application/json;charset=UTF-8
Server: BWS
{
    "name":"pool1",
    "ranges":[
        [
            {
                "subnet": "172.16.5.0/24"
            }
        ]
    ],
    "allocated": [
        "192.168.1.1",
        "192.168.1.2",
        "192.168.1.3"
    ]
}