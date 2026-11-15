# Deploying HRMS on a small paid server

This manual puts the whole app on one Linux server (Hetzner, DigitalOcean, AWS Lightsail or similar) with HTTPS. Plan on about an hour the first time.

What gets deployed: attendance with location and network checks, WFH and other requests, timesheets, task boards, chat and notifications. Everyone signs in with a password plus a code from an authenticator app. Calls and mail aren't built yet.

## How it fits together

```
Phone / laptop ──HTTPS──▶ Caddy (ports 80, 443)
                           ├─ /api/*, /healthz, /api/live (WebSocket) ──▶ api  (Go, :8180)
                           └─ everything else ─────────────────────────▶ web  (Next.js, :3100)
                                                      api ──▶ postgres (data)
                                                      api ──▶ redis    (rate limits, live events)
```

- Everything runs in Docker from `deploy/docker-compose.yml`.
- Only Caddy is open to the internet. Postgres, Redis, the API and the web app sit on a private Docker network.
- Caddy gets and renews a free Let's Encrypt certificate by itself.
- The API applies database migrations every time it starts.

---

## 1. Before you start

Have these ready:

| You need | Why |
|---|---|
| A server: Ubuntu 24.04 LTS, 2 vCPU, **4 GB RAM**, 40 GB disk | Building the web app needs about 2 GB of memory. With 2 GB RAM, add swap (step 3.5). |
| A domain or subdomain, for example `hr.yourcompany.com` | HTTPS needs a name. Phones only share location with HTTPS sites. |
| The office's **public IP address(es)** | The "office network" check compares each check-in with this. Ask the ISP for a **static IP**; a changing IP will block check-ins. |
| The office's map location | For the geofence (the circle around the office). |
| The first Admin's email and name | Created in step 8. This person becomes the **Owner**. |
| A phone for every person, with an authenticator app | Everyone signs in with a 6-digit code from Google Authenticator or Microsoft Authenticator (both free). |
| Access to the GitHub repo `nagasai-2003/HRMS` | To copy the code to the server. |

Rough prices (check the provider's page, they change): Hetzner 2 vCPU / 4 GB is about €4–6 a month. DigitalOcean or Lightsail with a Mumbai or Bangalore region cost more for 4 GB; their 2 GB plans plus swap also work.

---

## 2. Create the server

1. In the provider's panel, create a server with **Ubuntu 24.04 LTS** and the size above. Pick the region nearest the office.
2. Add your **SSH public key** when asked. Don't use password login.
3. If the provider has a firewall in its panel, allow inbound **22, 80, 443 (TCP)** and **443 (UDP)** only.
4. Note the server's public IPv4 address. Below it is `SERVER_IP`.

---

## 3. Prepare the server

Log in:

```bash
ssh root@SERVER_IP
```

### 3.1 Updates

```bash
apt update && apt -y upgrade && apt -y install git ufw unattended-upgrades
```

```bash
dpkg-reconfigure -plow unattended-upgrades
```

### 3.2 A deploy user

```bash
adduser --disabled-password --gecos "" deploy
```

```bash
usermod -aG sudo deploy && mkdir -p /home/deploy/.ssh && cp ~/.ssh/authorized_keys /home/deploy/.ssh/ && chown -R deploy:deploy /home/deploy/.ssh && chmod 700 /home/deploy/.ssh
```

Give `deploy` a sudo password (`passwd deploy`), then check from your own computer that `ssh deploy@SERVER_IP` works **before** the next step.

### 3.3 Turn off root and password logins

```bash
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin no/; s/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config && systemctl restart ssh
```

### 3.4 Firewall

```bash
ufw allow OpenSSH && ufw allow 80/tcp && ufw allow 443/tcp && ufw allow 443/udp && ufw --force enable
```

Note: Docker publishes ports past `ufw`. That's fine here because only Caddy publishes ports (80, 443). **Never add a `ports:` line to postgres or redis.**

### 3.5 Swap (only on 2 GB servers)

```bash
fallocate -l 4G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo '/swapfile none swap sw 0 0' >> /etc/fstab
```

### 3.6 Time zone

The app's own time zone is set inside the app (step 9). Setting the server's too makes logs easier to read:

```bash
timedatectl set-timezone Asia/Kolkata
```

---

## 4. Install Docker

As `deploy`:

```bash
curl -fsSL https://get.docker.com | sudo sh
```

```bash
sudo usermod -aG docker deploy
```

Log out and back in, then check:

```bash
docker run --rm hello-world && docker compose version
```

---

## 5. Point the domain at the server

At your DNS provider, add a record:

| Type | Name | Value |
|---|---|---|
| A | `hr` (for `hr.yourcompany.com`) | `SERVER_IP` |

- **If the DNS is on Cloudflare, set the record to "DNS only" (grey cloud), not "Proxied".** With the proxy on, the app sees Cloudflare's IP instead of the office's, and every office check-in fails the network check.
- Wait until it resolves. From your computer, `nslookup hr.yourcompany.com` must print `SERVER_IP`.
- Do this **before** step 7, or Caddy can't get the certificate.

---

## 6. Get the code

The repo is private, so give the server a read-only deploy key:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/hrms_deploy -N "" && cat ~/.ssh/hrms_deploy.pub
```

On GitHub: repo **Settings → Deploy keys → Add deploy key**, paste the key, leave "Allow write access" **off**.

```bash
printf 'Host github.com\n  IdentityFile ~/.ssh/hrms_deploy\n' >> ~/.ssh/config
```

```bash
git clone git@github.com:nagasai-2003/HRMS.git ~/hrms
```

---

## 7. Settings and first start

```bash
cd ~/hrms/deploy && cp .env.example .env && chmod 600 .env
```

Make the passwords:

```bash
openssl rand -hex 24
```

Run it twice: one value for `POSTGRES_PASSWORD`, one for `SEED_PASSWORD`. Then make the authenticator key:

```bash
openssl rand -hex 32
```

Edit the file (`nano .env`):

| Setting | Value |
|---|---|
| `SITE_ADDRESS` | `hr.yourcompany.com` (no `https://`) |
| `POSTGRES_PASSWORD` | the first random value |
| `SEED_PASSWORD` | the second value. This becomes the first Admin's password, so save it in your password manager. |
| `MFA_KEY` | the 64-character value. It encrypts everyone's authenticator secret. **Save a copy in your password manager, apart from the backups.** |
| `MFA_ISSUER` | the name people see in their authenticator app, for example your company name |
| `ALLOWED_ORIGINS` | leave empty |
| `HTTP_PORT`, `HTTPS_PORT` | keep `80` and `443` |

Start everything (the first build takes 5–10 minutes):

```bash
docker compose up -d --build
```

```bash
docker compose ps
```

All five services (postgres, redis, api, web, caddy) should say `running` or `healthy`. Then:

```bash
curl -s https://hr.yourcompany.com/healthz
```

It should answer from the API. If it doesn't, see **Troubleshooting** (section 15).

Don't change `POSTGRES_PASSWORD` after this first start: Postgres keeps the first one. To change it later you must change it inside the database too.

**Never change or lose `MFA_KEY`.** Without the same key, no authenticator code works and everyone has to set up their app again (see section 10, "MFA_KEY was lost").

Codes depend on the clock, so check the server's time is synced. `timedatectl` must show `System clock synchronized: yes`.

---

## 8. Create the first Admin

```bash
docker compose run --rm api seed -first-admin -email admin@yourcompany.com -name "Full Name"
```

It prints `Admin … created as the Owner`. It does nothing if anyone already exists.

> **Never run `seed` without `-first-admin` on this server.** On its own it creates the demo company: fake people with demo emails, all sharing one password, and test office networks.

Now remove the password from the server:

1. Delete the `SEED_PASSWORD=` line from `deploy/.env`.
2. Apply it: `docker compose up -d`.

Sign in at `https://hr.yourcompany.com` with the Admin email and that password. The app then asks you to set up your authenticator:

1. Install Google Authenticator or Microsoft Authenticator on your phone.
2. Scan the QR code on the screen.
3. Type the 6-digit code the app shows.
4. **Save the 10 recovery codes** it shows (download or copy them). Each one signs you in once if you lose your phone.

There's no "change my password" page yet: the Admin sets and changes passwords on **Teams and people** (edit a person, fill in "New password").

---

## 9. Set up the company in the app

Sign in as the Admin and work through these in order.

### 9.1 Attendance rules (Admin → Attendance rules)

- **Time zone:** `Asia/Kolkata` (or the office's IANA time zone). Shift start, late time, auto check-out (23:59 by default) and WFH quota (2 a month by default) are on the same page.
- **Office locations:** standing in the office, open this page on a phone, press **Use my location**, give it a name and a radius, then **Add office**. 150 m is a good start; indoor GPS can be off by 50–100 m.
- **Office networks:** connected to the office Wi-Fi, press **Use my current network**. The card also shows "Your network now". Add every office line (main and backup ISP). A range such as `203.0.113.0/29` works too.
- **Both / either:** "both" is the safest (location **and** office network). Pick "either" if the office Wi-Fi or indoor GPS is unreliable.

### 9.2 People and teams (Admin → Teams and people)

- Create the teams, then add people with their role (Employee, HR, Admin), work policy (office, hybrid with remote weekdays, or remote), office and a first password (10+ characters). Share each first password privately.
- Tick **Manages this team** for each team's manager.
- Set each team's key (for task numbers like `FE-142`).

### 9.3 Holidays and projects (Admin → Holidays and projects)

Add the year's company holidays and the timesheet projects people log hours against.

### 9.4 Tell everyone how to sign in

Each person signs in with the email and first password you gave them. At their first sign-in the app walks them through setting up an authenticator app and saving recovery codes. On **Teams and people**, "No authenticator yet" shows who hasn't done it.

---

## 10. Owner, authenticator and lost phones

### The Owner

- The Owner is an Admin that **no other Admin can change**: not their role, password, active status or authenticator.
- There is exactly one Owner: the first Admin from step 8.
- To hand the role over, the Owner opens **Teams and people**, edits another Admin who has set up their authenticator, and confirms **Make Owner** with a code from their own app. The old Owner stays an Admin.

### Someone lost or changed their phone

| Situation | What to do |
|---|---|
| They still have a recovery code | On the code screen, press "Lost your phone? Use a recovery code". Then make new codes on **Sign-in security**. |
| No recovery codes left | An Admin opens **Teams and people**, edits the person and presses **Reset authenticator**. The person sets the app up again at their next sign-in. |
| The **Owner** lost the phone and every recovery code | Only the server can reset it. On the server: `cd ~/hrms/deploy && docker compose run --rm api seed -reset-mfa -email owner@yourcompany.com` |

### `MFA_KEY` was lost

Restore it from the password manager. If there is no copy, clear every authenticator so people set them up again:

```bash
docker compose exec postgres psql -U hrms hrms -c "UPDATE users SET mfa_secret = NULL, mfa_enabled_at = NULL, mfa_last_step = 0; DELETE FROM mfa_recovery_codes; DELETE FROM sessions;"
```

Then put a new `MFA_KEY` in `deploy/.env` and run `docker compose up -d`.

---

## 11. Check it works

Do these once, and again after each update:

- [ ] `https://hr.yourcompany.com` opens with a padlock, on a laptop and a phone.
- [ ] Sign-in asks for a code after the password, and a wrong code is refused.
- [ ] A new person is asked to set up the authenticator at their first sign-in and gets recovery codes.
- [ ] Sign-out works.
- [ ] **In the office, on office Wi-Fi:** an employee can check in, take a break and check out.
- [ ] **On mobile data, outside the office:** check-in is refused with a clear reason, and the refusal shows to their manager and HR.
- [ ] An approved WFH day lets the employee check in from home.
- [ ] Chat: two people in two browsers see each other's messages without reloading, and the green "online" dot shows.
- [ ] An @mention makes the bell show a notification.
- [ ] `docker compose logs api --since 10m` shows no errors.

---

## 12. Backups

Everything important is in Postgres. Back it up every night and keep copies **off the server**.

```bash
mkdir -p ~/backups
```

Add a nightly job with `crontab -e`:

```
30 2 * * * cd /home/deploy/hrms/deploy && docker compose exec -T postgres pg_dump -U hrms -Fc hrms > /home/deploy/backups/hrms-$(date +\%F).dump && find /home/deploy/backups -name 'hrms-*.dump' -mtime +14 -delete
```

That keeps 14 days on the server. A backup restored on another server only works with the same `MFA_KEY`, so keep the key safe as well (not in the same place as the backups). To copy them elsewhere, use the provider's snapshot or backup feature, or sync `~/backups` to object storage (for example with `rclone`).

**Test a restore at least once**, on a spare server or a copy:

```bash
cd ~/hrms/deploy && docker compose stop api web
```

```bash
docker compose exec -T postgres pg_restore -U hrms -d hrms --clean --if-exists < ~/backups/hrms-YYYY-MM-DD.dump
```

```bash
docker compose start api web
```

---

## 13. Updating to a new version

```bash
cd ~/hrms && git pull
```

```bash
cd deploy && docker compose up -d --build
```

- Take a backup first (run the `pg_dump` part of the cron line by hand).
- The API applies new migrations when it starts. Watch `docker compose logs -f api` for a minute.
- Pages are briefly unavailable while the new containers start.
- To roll back code: `git checkout <previous commit>` and rebuild. Migrations aren't undone automatically, so if a migration ran, restore the backup instead.
- Clear old build files now and then: `docker image prune -f` and `docker builder prune -f`. **Never run `docker volume prune`:** it deletes the database.

---

## 14. Everyday commands

All from `~/hrms/deploy`.

| To | Run |
|---|---|
| See what's running | `docker compose ps` |
| Follow API logs | `docker compose logs -f api` |
| Follow Caddy logs (HTTPS, requests) | `docker compose logs -f caddy` |
| Restart one service | `docker compose restart api` |
| Stop everything (data kept) | `docker compose down` |
| Start again | `docker compose up -d` |
| Open the database | `docker compose exec postgres psql -U hrms hrms` |
| Disk space | `df -h` and `docker system df` |

The containers restart on their own after a crash or a server reboot.

---

## 15. Troubleshooting

| Problem | Likely cause and fix |
|---|---|
| No padlock, or Caddy logs show certificate errors | The domain doesn't point at `SERVER_IP` yet, or ports 80/443 are closed in the provider's firewall. Fix it, then `docker compose restart caddy`. |
| Every office check-in says "not on an office network" | The office IP changed (no static IP), or the IP saved in Attendance rules is wrong, or Cloudflare's proxy is on (step 5). Open Attendance rules from office Wi-Fi and compare "Your network now". |
| Check-in says the location is too inaccurate or outside the office | Turn on precise location on the phone, step near a window, or raise the radius or the accuracy limit in Attendance rules. |
| "That code is not right" for everyone | The server clock is off. Check `timedatectl` and turn on time sync (`sudo timedatectl set-ntp true`). |
| "That code is not right" for one person | Their phone's clock is off: turn on automatic date and time. Or they scanned an old QR code: reset their authenticator (section 10). |
| API stops at start with `MFA_KEY` | `MFA_KEY` is missing from `deploy/.env` or isn't 64 hex characters. |
| Phone never asks for location | The site must be HTTPS, and the browser must allow location for it (check site settings). |
| Chat doesn't update live | Check `docker compose logs api` for WebSocket errors. Make sure nothing else in front of Caddy blocks WebSockets. If the pages are served from a different address than `SITE_ADDRESS`, list it in `ALLOWED_ORIGINS`. |
| Build stops with "killed" or out of memory | Add swap (step 3.5) or use a 4 GB server. |
| `docker compose up` says a network overlaps | Another network on the server uses `172.31.0.0/24`. Change the subnet and Caddy's `ipv4_address` in `docker-compose.yml`, and `TRUSTED_PROXIES` to match. |
| API keeps restarting | `docker compose logs api`. A wrong `POSTGRES_PASSWORD` or a failed migration shows there. |

---

## 16. Security rules for the team

- `deploy/.env` holds passwords. It is git-ignored; **never commit it** or paste it into chat.
- Keep only ports 22, 80 and 443 open. Don't publish Postgres or Redis.
- `TRUSTED_PROXIES` must stay as Caddy's address only. Widening it lets anyone fake their IP and pass the office network check.
- `APP_ENV=production` must stay set (secure cookies).
- Don't run the demo `seed` on this server (step 8).
- `MFA_KEY` is as secret as the database password. Keep one copy in a password manager.
- Anyone who can sign in to the server can reset any authenticator, the Owner's included (`seed -reset-mfa`). Give server access only to people the Owner trusts.
- When someone leaves, untick **Active** on their entry in Teams and people the same day.
- Keep the server patched (unattended-upgrades does security updates) and reboot when `/var/run/reboot-required` exists.
