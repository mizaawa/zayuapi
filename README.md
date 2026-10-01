# Sub2API Deployment

<p align="center">
  <a href="README.md"><img src="https://img.shields.io/badge/English-README-0969da" alt="English"></a>
  <a href="README_CN.md"><img src="https://img.shields.io/badge/%E7%AE%80%E4%BD%93%E4%B8%AD%E6%96%87-README-0969da" alt="简体中文"></a>
  <a href="README_JA.md"><img src="https://img.shields.io/badge/%E6%97%A5%E6%9C%AC%E8%AA%9E-README-0969da" alt="日本語"></a>
</p>

This repository is maintained at [mizaawa/zayuapi](https://github.com/mizaawa/zayuapi). Deployment images, install scripts, and the in-app version update and rollback feature all use this repository's releases.

This is a private repository. The deployment PAT is stored in the private repository's `deploy/.env`:

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi
bash deploy/start-private.sh
```

The startup script reads `.env`, authenticates Docker to GHCR, pulls the image and starts Compose. See [private repository deployment](deploy/PRIVATE_REPOSITORY.md) for deployment and release details.

## Docker Compose

Prerequisites: Docker Engine 20.10+ and Docker Compose v2+.

### One-command preparation

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy
bash start-private.sh
```

The startup script reads the private repository `.env` and Compose files, logs Docker into GHCR and starts the stack. Data stays in the local `data`, `postgres_data`, and `redis_data` directories.

Open `http://SERVER_IP:8080` after the containers become healthy. If no `ADMIN_PASSWORD` is set in `.env`, inspect the application log for the generated password.

### Manual deployment

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy
cp .env.example .env
chmod 600 .env
mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d
```

Before starting a production deployment, set strong values for `POSTGRES_PASSWORD`, `JWT_SECRET`, and `TOTP_ENCRYPTION_KEY` in `.env`. Optionally set `ADMIN_EMAIL`, `ADMIN_PASSWORD`, and `SERVER_PORT`.

### Migrate an existing upstream deployment

The local-directory Compose file stores application data in `./data`, PostgreSQL in `./postgres_data`, and Redis in `./redis_data`. Run the following from the **existing** deployment directory. Do not create a second directory and do not run `docker-deploy.sh` again, because it can replace `.env` and generate new secrets.

```bash
cd /path/to/sub2api-deploy
umask 077
STAMP=$(date +%Y%m%d-%H%M%S)
mkdir -p "backups/$STAMP"
cp .env docker-compose.yml "backups/$STAMP/"
docker compose exec -T postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "backups/$STAMP/sub2api.dump"

# Change only the sub2api service image; leave postgres and redis unchanged.
sed -i 's#image: weishaw/sub2api:latest#image: ${SUB2API_IMAGE:-ghcr.io/mizaawa/zayuapi:latest}#' docker-compose.yml
printf '\nSUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:latest\n' >> .env

docker compose config -q
docker compose pull sub2api
docker compose up -d --no-deps sub2api
docker compose ps
```

The command leaves the database and Redis containers running and recreates only `sub2api`. Keep the original `POSTGRES_PASSWORD`, `JWT_SECRET`, `TOTP_ENCRYPTION_KEY`, and any proxy/provider settings in `.env`.

### Update and rollback

Update Docker containers from the published image:

```bash
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d
```

To pin a specific published image version, set `SUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:<version>` in `.env`, then run the same two commands. The administrator console checks releases from `mizaawa/zayuapi`; its one-click update and rollback options require release assets published by this repository.

Update container deployments through Compose. In-place binary replacement is intended for systemd installations; recreating a container restores the image's binary. Full releases include platform binaries and amd64/arm64 images. Simple releases contain only the amd64 image and cannot supply in-app binary updates.

### Common commands

```bash
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs -f sub2api
docker compose -f docker-compose.local.yml restart
docker compose -f docker-compose.local.yml down
```
