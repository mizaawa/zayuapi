# Sub2API デプロイ

<p align="center">
  <a href="README.md"><img src="https://img.shields.io/badge/English-README-0969da" alt="English"></a>
  <a href="README_CN.md"><img src="https://img.shields.io/badge/%E7%AE%80%E4%BD%93%E4%B8%AD%E6%96%87-README-0969da" alt="简体中文"></a>
  <a href="README_JA.md"><img src="https://img.shields.io/badge/%E6%97%A5%E6%9C%AC%E8%AA%9E-README-0969da" alt="日本語"></a>
</p>

このリポジトリは [mizaawa/zayuapi](https://github.com/mizaawa/zayuapi) で管理されています。デプロイ用イメージ、インストールスクリプト、管理画面のワンクリック更新とロールバックは、すべてこのリポジトリの Release を使用します。

デプロイ用 PAT は非公開リポジトリの `deploy/.env` に設定済みです。

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi
bash deploy/start-private.sh
```

起動スクリプトが `.env` を読み込み、GHCR ログイン、イメージ取得、Compose 起動を行います。詳細は [非公開リポジトリのデプロイ](deploy/PRIVATE_REPOSITORY.md) を参照してください。

## Docker Compose デプロイ

必要環境：Docker Engine 20.10 以降、Docker Compose v2 以降。

### ワンクリック準備

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy
bash start-private.sh
```

起動スクリプトは非公開リポジトリの `.env` と Compose ファイルを読み込み、GHCR にログインしてサービスを起動します。データはローカルの `data`、`postgres_data`、`redis_data` に保存されます。

コンテナが正常になったら `http://SERVER_IP:8080` を開きます。`.env` の `ADMIN_EMAIL` または `ADMIN_PASSWORD` が空の場合は、アプリケーションログで自動生成された管理者メールアドレス（ログインユーザー名）とパスワードを確認してください。

### 手動デプロイ

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy
cp .env.example .env
chmod 600 .env
mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d
```

本番環境では、起動前に `.env` の `POSTGRES_PASSWORD`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY` に強力なランダム値を設定してください。必要に応じて `ADMIN_EMAIL`、`ADMIN_PASSWORD`、`SERVER_PORT` も設定できます。管理者メールアドレスはログイン可能な有効な形式、パスワードは 8-72 バイトである必要があります。アップグレードでは既存のアカウントは変更されません。

### 既存の上流版デプロイからデータを保持して移行

ローカルディレクトリ版 Compose は、アプリケーションデータを `./data`、PostgreSQL を `./postgres_data`、Redis を `./redis_data` に保存します。以下の操作は必ず**既存のデプロイディレクトリ**で実行してください。別のディレクトリを作成したり、`docker-deploy.sh` を再実行したりしないでください。`.env` が置き換えられ、新しい秘密鍵が生成される可能性があります。

```bash
cd /path/to/sub2api-deploy
umask 077
STAMP=$(date +%Y%m%d-%H%M%S)
mkdir -p "backups/$STAMP"
cp .env docker-compose.yml "backups/$STAMP/"
docker compose exec -T postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "backups/$STAMP/sub2api.dump"

# sub2api のイメージだけを変更し、postgres と redis は変更しない
sed -i 's#image: weishaw/sub2api:latest#image: ${SUB2API_IMAGE:-ghcr.io/mizaawa/zayuapi:latest}#' docker-compose.yml
printf '\nSUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:latest\n' >> .env

docker compose config -q
docker compose pull sub2api
docker compose up -d --no-deps sub2api
docker compose ps
```

この手順では `sub2api` コンテナだけを再作成し、PostgreSQL と Redis のコンテナやデータディレクトリは削除しません。既存の `.env` にある `POSTGRES_PASSWORD`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY`、プロキシ、プロバイダー設定はそのまま保持してください。

### 更新とロールバック

```bash
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d
```

特定のイメージバージョンに固定またはロールバックする場合は、`.env` に `SUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:<version>` を設定して同じコマンドを実行します。管理画面のワンクリック更新とロールバックは `mizaawa/zayuapi` の GitHub Releases からバージョンを取得します。

コンテナは Compose でイメージを更新してください。バイナリの置き換えは systemd インストール向けです。Full Release はバイナリと amd64/arm64 イメージを公開します。Simple Release は amd64 イメージのみで、管理画面のバイナリ更新には使用できません。

### よく使うコマンド

```bash
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs -f sub2api
docker compose -f docker-compose.local.yml restart
docker compose -f docker-compose.local.yml down
```
