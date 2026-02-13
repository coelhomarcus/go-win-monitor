# Agente de Monitoramento de PC (Windows)

Aplicativo leve de bandeja do sistema que monitora CPU, RAM e GPU NVIDIA, enviando métricas para uma API remota.

### Funcionalidades
- Mostra uso de CPU, RAM e GPU na bandeja do sistema.
- Envia métricas periodicamente para um endpoint de API configurável.
- Executável GUI para Windows (sem janela de console).

### Requisitos
- Go 1.20+
- Windows
- GPU NVIDIA (opcional, para monitoramento de GPU)

### Variáveis de Ambiente
- `M_API_URL` -> Endpoint da API para envio das métricas
- `M_AGENT_SECRET` -> Token Bearer para autenticação na API

### Build
```
.\build.ps1
```
