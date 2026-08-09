# Despliegue de Gotenberg en el VPS (staging/prod)

Gotenberg es un servicio sin estado (stateless): alcanza con **un solo contenedor
compartido** entre `etick-firmador-staging` y `etick-firmador-prod` en el VPS OCI
(`147.15.75.118`). No se versiona en este repo porque los scripts `deploy-staging.sh`
y `deploy-main.sh` viven directamente en el servidor (`/home/opc/`), no en el repo.

## Paso manual (una sola vez, por SSH)

```bash
ssh -i <key> opc@147.15.75.118

# 1) Levantar Gotenberg (si no existe ya el contenedor)
sudo docker run -d \
  --name firmador-gotenberg \
  --restart unless-stopped \
  -p 3000:3000 \
  gotenberg/gotenberg:8

# 2) Verificar
curl -sS http://127.0.0.1:3000/health
```

## Variable de entorno en los contenedores del firmador

Agregar `GOTENBERG_URL=http://172.17.0.1:3000` (o la IP del host/bridge de Docker
visible desde los contenedores `etick-firmador-staging`/`-prod`; alternativamente
unir ambos contenedores a una red Docker común y usar `http://firmador-gotenberg:3000`)
a los `docker run ...` de `deploy-staging.sh` / `deploy-main.sh` en el servidor.

Sin esta variable, `GOTENBERG_URL` cae al default `http://localhost:3000` (solo
sirve en desarrollo local con `docker compose up -d gotenberg`). Si Gotenberg no
responde, la generación del KuDE falla silenciosamente (best-effort, asíncrona) y
la emisión SIFEN **no se ve afectada**: solo queda pendiente el PDF.
