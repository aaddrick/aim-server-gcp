# Public configuration — committed. Everything here is discoverable from
# public DNS once the server is live (the hostname, the DKIM public key,
# etc.), so there is nothing to protect. Account-identifying values live in
# private.auto.tfvars (gitignored); see private.auto.tfvars.example.

region = "us-central1" # free-tier e2-micro region
zone   = "us-central1-a"

# The server: OSCAR on 5190 and the signup site on 443, same hostname.
# The Cloud DNS zone covers just this subdomain; the parent domain
# delegates to it with a single NS record set.
server_fqdn   = "aim.aaddrick.com"
dns_zone_root = "aim.aaddrick.com"
dns_zone_name = "aim"

# From the Resend dashboard after adding aim.aaddrick.com as a domain
# (public key — it gets published as a DNS TXT record either way).
resend_dkim_key = "p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDmljaZdQN/I7b1zlWwkFIm79r8qJcWILhZLNWEOqzZ/Kche0nDONdS3fsxDUJmsrGEF9z453ZSUDWh1r5KX+HD8xIt7xItLh0YIY08auy0c6OTRITGnDxLmSr5L/wNx238EAentKOor8a0lCdcOu0VsDhF3C0ho0uCQsJN4aVsiQIDAQAB"

# enable_toc = true # uncomment for TOC clients (port 9898)
