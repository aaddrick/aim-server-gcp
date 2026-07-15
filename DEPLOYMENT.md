# Deployment

How to stand up the whole stack from nothing. One VM, one domain, ~$3/month.

## Prerequisites

- A GCP project with billing (the always-free e2-micro needs `us-west1`,
  `us-central1`, or `us-east1`)
- `gcloud` authenticated as a project owner
- A domain (or subdomain) you can delegate to Cloud DNS
- A [Resend](https://resend.com) account (free tier: 3,000 emails/month)

## 1. Bootstrap the Terraform state bucket (one time)

Versioning is required. It's what makes the GCS backend's native state
locking work, and it gives you state-file recovery.

```sh
gcloud storage buckets create gs://YOUR_PROJECT-tfstate --project=YOUR_PROJECT \
  --location=us-central1 --uniform-bucket-level-access
gcloud storage buckets update gs://YOUR_PROJECT-tfstate --versioning
gcloud storage buckets update gs://YOUR_PROJECT-tfstate --public-access-prevention
```

Put the bucket name in `terraform/backends/prod.hcl`.

## 2. First apply

```sh
cd terraform
cp private.auto.tfvars.example private.auto.tfvars   # project_id + alert_email
# edit config.auto.tfvars: server_fqdn, dns_zone_root, dns_zone_name
terraform init -backend-config=backends/prod.hcl
terraform apply
```

The `zone_nameservers` output lists four `ns-cloud-*.googledomains.com`
hosts. Add them as an NS record set for your subdomain at the parent domain
(wherever its DNS lives) to delegate the zone. `dig +short A <server_fqdn>`
confirms delegation once it propagates.

> First-run note: if the apply races Compute Engine API activation on a
> brand-new project, wait a minute and re-apply. It's idempotent.

## 3. Resend

Add your `server_fqdn` as a domain in the
[Resend dashboard](https://resend.com/domains) (region `us-east-1` matches
the MX record Terraform creates), copy the DKIM public key into
`resend_dkim_key` in `config.auto.tfvars`, and `terraform apply` again.

Check the records, then hit **Verify DNS Records** in Resend:

```sh
./deploy/verify-email-dns.sh aim.example.com
```

Create an API key (Sending access is enough) and store it. This is the
only secret in the system, and it never touches Terraform or git:

```sh
printf '%s' 're_xxxxxxxx' | gcloud secrets versions add RESEND_API_KEY --data-file=-
```

## 4. Provision the VM

```sh
gcloud compute ssh aim-server --tunnel-through-iap --zone us-central1-a --command="mkdir -p ~/aim"
gcloud compute scp --recurse deploy signup aim-server:~/aim --tunnel-through-iap --zone us-central1-a
gcloud compute ssh aim-server --tunnel-through-iap --zone us-central1-a
sudo ~/aim/deploy/setup.sh --domain aim.example.com --bucket YOUR_PROJECT-aim-backups
```

`setup.sh` is idempotent. Re-run it to pick up new open-oscar-server
releases or config changes. It installs the latest upstream release binary,
builds the signup service from source, configures Caddy (which provisions
its Let's Encrypt certificate automatically once DNS resolves), and enables
all the systemd units.

## 5. Chat

Point AIM 5.1 (or Pidgin, or anything OSCAR-speaking) at
`aim.example.com` port `5190`. Send friends to `https://aim.example.com`
to sign up.

## Operations

- **Logs:** `journalctl -u openoscar -u aim-signup -f`
- **Backups:** daily timer → `gs://…-aim-backups` (90-day retention);
  restore = stop `openoscar`, gunzip over `/var/lib/openoscar/oscar.sqlite`, start.
- **Manual account:** `curl -d '{"screen_name":"Foo","password":"bar1"}' http://127.0.0.1:8080/user` on the VM.
- **Monitoring:** TCP uptime check on 5190 emails `alert_email` after 10 minutes down.
- **Upgrades:** re-run `setup.sh` (fetches the latest upstream release).

## Security notes

- The management API has **no authentication**. It must never leave
  127.0.0.1. The firewall only opens 5190, 80/443, and (optionally) 9898.
- OSCAR is a 1990s protocol: client traffic is plaintext and password
  hashing is era-authentic (weak). The reset page warns users not to reuse
  a real password. Treat every AIM password as public-adjacent.
- The signup service never stores a password. Accounts are created with a
  generated one shown once after email verification; users change it from
  inside the client (Change Password in AIM/Pidgin) or via `/reset`, which
  emails a one-hour single-use link to the address that verified the
  account. The 0600 state file holds only pending tokens and the screen
  name → verified email ledger that resets depend on.
- Accounts created by hand with `curl` have no email on file, so `/reset`
  won't serve them; in-client password change still works.
- Signup abuse controls: per-IP rate limit (10 per 10 minutes, 10-minute
  ban), a honeypot form field, and a daily email cap (`--email-cap`,
  default 90 to suit Resend's free tier; raise it on a paid plan).
- SSH is IAP-only (`gcloud compute ssh aim-server --tunnel-through-iap`).
