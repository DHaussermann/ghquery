# Deploying ghquery (hosted mode) to EC2

Terraform that stands up a single EC2 instance running ghquery in hosted mode,
reachable over Tailscale. Matches the design in
[`../HOSTED-MODE-DESIGN.md`](../HOSTED-MODE-DESIGN.md).

## What it provisions

- One `t3.small` EC2 instance (Ubuntu 24.04), with an optional Elastic IP.
- A security group whose **only** public ingress is SSH from your admin CIDR.
  The web UI port is never exposed publicly — you reach it over Tailscale.
- `cloud-init` (user_data) that installs Tailscale, creates the `ghquery`
  service user and directories, writes a starter `catalog.yaml` and a secrets
  env file, and installs a `systemd` service.

What it deliberately does **not** do: hold any secrets (you fill those in on the
box), expose the app to the internet, or auto-upgrade the binary (you control
that by hand — see Upgrades).

## Prerequisites

- Terraform ≥ 1.3 and AWS credentials (`aws configure` or env vars).
- An existing EC2 key pair (`key_name`).
- A Tailscale account (for access) and, optionally, an auth key.
- The `ghquery` linux-amd64 binary, built from this branch:
  `GOOS=linux GOARCH=amd64 go build -o ghquery-linux-amd64 .`

## Apply

```sh
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars   # edit key_name, admin_ssh_cidr, ...
terraform init
terraform apply
```

`terraform output next_steps` prints the post-apply checklist.

## Post-apply (one-time setup over SSH)

The instance boots configured but inert until you give it a binary, secrets, and
the agent prompt:

```sh
ssh ubuntu@<public_ip>
sudo tailscale up --ssh                       # if you didn't pass tailscale_auth_key

# Binary + the .claude prompt directory (see note below):
scp ghquery-linux-amd64 ubuntu@<public_ip>:/tmp/ghquery
sudo install -m 0755 /tmp/ghquery /usr/local/bin/ghquery
scp -r .claude ubuntu@<public_ip>:/tmp/.claude
sudo cp -r /tmp/.claude/agents /opt/ghquery/.claude/
sudo chown -R ghquery:ghquery /opt/ghquery

sudo nano /etc/ghquery/ghquery.env            # fill GITHUB_TOKEN + ANTHROPIC_API_KEY
sudo nano /etc/ghquery/catalog.yaml           # your repos + teams
sudo systemctl restart ghquery
sudo systemctl status ghquery                 # confirm it's running
```

Then open `http://<tailscale-ip>:8080` from any device on the tailnet.

> **Why `.claude` has to be on the box:** the risk-scoring pass reads its prompt
> from `.claude/agents/risk-analyzer.md` **relative to the working directory**
> (`/opt/ghquery`). The binary alone is not enough — copy that file too. It
> changes rarely, so this is a one-time copy; routine binary upgrades don't need
> it re-copied. (A future improvement would embed the prompt in the binary via
> `go:embed` so deploys are fully self-contained.)

## Upgrades (manual)

```sh
ssh ubuntu@<public_ip>
# build a new ghquery-linux-amd64 locally and scp it, or wget a release:
scp ghquery-linux-amd64 ubuntu@<public_ip>:/tmp/ghquery
sudo install -m 0755 /tmp/ghquery /usr/local/bin/ghquery
sudo systemctl restart ghquery
```

## Notes & decisions

- **No public ingress to the app.** Access is over Tailscale (WireGuard,
  end-to-end encrypted), so the first cut runs plain HTTP on the tailnet — no
  TLS/cert management. Revisit TLS (e.g. Tailscale Serve, or Let's Encrypt) once
  a login exists and there's a reason to be publicly reachable.
- **Secrets stay on the box.** `GITHUB_TOKEN` and `ANTHROPIC_API_KEY` live only
  in `/etc/ghquery/ghquery.env` (mode 600), never in Terraform or its state.
  Hardening later: move them to AWS Secrets Manager and fetch at boot.
- **SSH is the one open port.** Locked to `admin_ssh_cidr`. Once Tailscale SSH
  works you can tighten or remove it.
- **Analysis backend.** With `ANTHROPIC_API_KEY` set, ghquery uses the Anthropic
  API (model `ANTHROPIC_MODEL`, default `claude-opus-4-8`). This path has not yet
  been exercised against a live key — verify after the first deploy.

## Tear down

```sh
terraform destroy
```
