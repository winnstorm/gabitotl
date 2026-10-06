# GabitoTL — Instalador, detección y migración segura

> **Objetivo:** instalar GabitoTL de forma automática, detectar los coding agents disponibles, preservar todo conocimiento útil existente y ofrecer una migración suave desde Gentle-AI u otros harness sin perder memoria, configuración o trazabilidad.

---

# 1. Idea principal

El instalador de GabitoTL no debe comportarse como un instalador tradicional que simplemente copia archivos.

Debe hacer cuatro cosas:

```text
DETECTAR
   ↓
PRESERVAR
   ↓
MIGRAR
   ↓
CONFIGURAR
```

La regla principal es:

> **Nunca borrar primero y preguntar después.**

Si encuentra Gentle-AI, Engram, Kiro u otra configuración existente, primero debe entender qué hay, crear una copia de seguridad, registrar qué va a conservar y recién después ofrecer cambios.

---

# 2. Experiencia deseada

El usuario ejecuta:

```bash
gabitotl
```

o:

```bash
gabitotl install
```

y aparece una TUI simple:

```text
╭────────────────────────────────────────╮
│              GabitoTL                  │
│     Preparando tu entorno de trabajo   │
╰────────────────────────────────────────╯

Sistema
  ✓ Linux
  ✓ Git
  ✓ Proyecto detectado: ecommerce

Coding agents
  ✓ Claude Code
  ✓ OpenCode
  ✓ Kiro CLI
  ✓ Kiro IDE

Memoria
  ✓ Engram encontrado
  ✓ 1.248 recuerdos existentes

Otros harness
  ! Gentle-AI encontrado

¿Qué querés hacer?

  ❯ Migrar Gentle-AI → GabitoTL
    Instalar GabitoTL sin tocar Gentle-AI
    Revisar detalles
    Salir
```

La experiencia tiene que ser entendible por alguien que no quiera conocer detalles internos.

---

# 3. Tecnología sugerida para el instalador

Recomiendo construir el instalador como **un único binario en Go**.

Ventajas:

- binario único;
- macOS, Linux y Windows;
- buena interacción con filesystem y procesos;
- fácil detección de executables;
- distribución sencilla;
- no necesita Python ni Node;
- la misma tecnología puede usarse luego para el core de GabitoTL.

Para la TUI:

```text
Bubble Tea
Lip Gloss
Huh
```

La TUI debe ser sólo la presentación.

Toda la lógica importante debe vivir fuera de la interfaz para que también exista:

```bash
gabitotl install --yes
gabitotl install --dry-run
gabitotl migrate gentle-ai --dry-run
gabitotl doctor
```

---

# 4. Comandos principales

```bash
gabitotl
```

Abre la TUI.

```bash
gabitotl install
```

Instalación interactiva.

```bash
gabitotl install --dry-run
```

Analiza todo pero no cambia nada.

```bash
gabitotl doctor
```

Revisa la instalación sin modificar nada.

```bash
gabitotl migrate
```

Busca harness anteriores y ofrece migrarlos.

```bash
gabitotl migrate gentle-ai
```

Migra específicamente desde Gentle-AI.

```bash
gabitotl restore
```

Permite volver al estado anterior.

```bash
gabitotl uninstall
```

Desinstala únicamente assets administrados por GabitoTL.

---

# 5. Fases del instalador

```text
START
  │
  ▼
1. Detectar plataforma
  │
  ▼
2. Detectar proyecto
  │
  ▼
3. Detectar runtimes
  │
  ▼
4. Detectar memoria
  │
  ▼
5. Detectar otros harness
  │
  ▼
6. Construir inventario
  │
  ▼
7. Mostrar plan
  │
  ▼
8. Crear snapshot
  │
  ▼
9. Migrar conocimiento
  │
  ▼
10. Retirar config anterior
  │
  ▼
11. Instalar GabitoTL
  │
  ▼
12. Configurar runtimes
  │
  ▼
13. Verificar memoria
  │
  ▼
14. Verificar proyecto
  │
  ▼
15. Mostrar resultado
```

Ninguna etapa destructiva ocurre antes del snapshot.

---

# 6. Detección de plataforma

Detectar:

```text
OS
arquitectura
shell
HOME
Git
package manager
PATH
```

Plataformas objetivo iniciales:

```text
macOS
Linux
Windows
```

Linux:

```text
apt
dnf
pacman
brew/linuxbrew
```

Windows:

```text
winget
choco       # opcional
scoop       # opcional
```

El instalador no debería depender de un package manager específico para funcionar.

---

# 7. Autodetección de coding agents

V1 debe detectar:

```text
Claude Code
OpenCode
Kiro CLI
Kiro IDE
```

No alcanza con revisar sólo `PATH`.

Cada adapter debe utilizar varias señales.

---

# 8. Claude Code

Señales:

```text
binary: claude
config: ~/.claude/
```

Windows:

```text
%USERPROFILE%\.claude\
```

Estado conceptual:

```text
Claude Code
✓ binary
✓ config
✓ usable
```

o:

```text
Claude Code
✗ binary
✓ configuración vieja encontrada
```

GabitoTL debe distinguir entre:

```text
installed
configured
detected-history
```

---

# 9. OpenCode

Señales principales:

```text
binary: opencode

global:
~/.config/opencode/

project:
opencode.json
opencode.jsonc
.opencode/
```

También respetar:

```text
XDG_CONFIG_HOME
OPENCODE_CONFIG
OPENCODE_CONFIG_DIR
```

El instalador debe leer configuración existente antes de modificarla.

Nunca reemplazar todo el JSON.

Debe hacer merge sólo de la parte administrada por GabitoTL.

---

# 10. Kiro CLI

Señales:

```text
binary:
kiro-cli
```

Configuración actual:

```text
~/.kiro/settings/cli.json
```

También considerar:

```text
KIRO_HOME
```

Kiro CLI y Kiro IDE comparten parte de:

```text
~/.kiro/
```

por lo que el instalador debe evitar tratar ambos como instalaciones totalmente independientes.

---

# 11. Kiro IDE

Señales:

```text
~/.kiro/
```

más instalación de la aplicación según plataforma.

Configuración compartida relevante:

```text
~/.kiro/agents/
~/.kiro/skills/
~/.kiro/steering/
~/.kiro/settings/mcp.json
```

Configuración de proyecto:

```text
<project>/.kiro/
```

El instalador debe diferenciar:

```text
Kiro CLI presente
Kiro IDE presente
Kiro workspace configurado
```

---

# 12. Pantalla de runtimes

Ejemplo:

```text
Coding agents encontrados

[x] Claude Code     listo
[x] OpenCode        listo
[x] Kiro CLI        listo
[x] Kiro IDE        listo

Configurar GabitoTL en:

[x] Claude Code
[x] OpenCode
[x] Kiro CLI
[x] Kiro IDE

Continuar
```

Por defecto seleccionar los detectados.

No intentar instalar automáticamente una aplicación pesada como Kiro IDE sin consentimiento.

Si falta una herramienta:

```text
Kiro CLI no está instalado.

[ Instalar ]
[ Omitir ]
```

---

# 13. Detección de Gentle-AI

Buscar como mínimo:

```text
binary:
gentle-ai

state:
~/.gentle-ai/state.json

home:
~/.gentle-ai/

managed config:
según installed_agents del state.json
```

El archivo:

```text
~/.gentle-ai/state.json
```

es especialmente útil porque permite conocer qué agentes fueron administrados por Gentle-AI.

El instalador debe leerlo antes de decidir qué limpiar.

Ejemplo interno:

```text
Gentle-AI detected
version: 4.x
agents:
- claude-code
- opencode
- kiro-ide

components:
- persona
- engram integration
- skills
...
```

---

# 14. Gentle-AI y Engram deben tratarse por separado

Esto es fundamental.

Conceptualmente:

```text
Gentle-AI
    │
    ├── configura agentes
    ├── instala prompts/skills
    ├── configura MCP
    │
    └── conecta Engram
                │
                ▼
             ENGRAM
          dueño real de
            la memoria
```

Por lo tanto:

> Desinstalar Gentle-AI NO debe significar eliminar Engram.

GabitoTL debe detectar Engram de forma independiente.

---

# 15. Detección de Engram

Buscar:

```text
binary:
engram
```

Data dir:

```text
ENGRAM_DATA_DIR
```

fallback:

```text
~/.engram/
```

Base habitual:

```text
~/.engram/engram.db
```

También revisar el proyecto actual:

```text
.engram/
.engram/config.json
.engram/manifest.json
```

Ejecutar diagnóstico read-only cuando sea posible:

```bash
engram doctor
```

Y consultar proyectos:

```bash
engram projects list
```

---

# 16. Soft landing desde Gentle-AI

El flujo recomendado:

```text
Gentle-AI encontrado
        │
        ▼
leer state.json
        │
        ▼
detectar Engram
        │
        ▼
validar Engram
        │
        ▼
exportar memoria de seguridad
        │
        ▼
capturar documentación/config útil
        │
        ▼
crear migration snapshot
        │
        ▼
crear .gabitotl
        │
        ▼
importar conocimiento
        │
        ▼
configurar GabitoTL
        │
        ▼
verificar que Engram sigue funcionando
        │
        ▼
recién ahora:
ofrecer retirar Gentle-AI
```

Importante:

**GabitoTL se instala y se verifica antes de retirar Gentle-AI.**

Eso reduce enormemente el riesgo.

---

# 17. No migrar Engram si no hace falta

Si el usuario elige:

```text
GabitoTL
modo individual
Engram
```

y ya tiene Engram funcionando:

```text
NO importar/exportar hacia una base nueva
NO crear otra base
NO copiar recuerdos individualmente
```

GabitoTL simplemente adopta la memoria existente.

```text
Gentle-AI ──X
             \
              misma ~/.engram/engram.db
                         │
                         ▼
                     GabitoTL
```

Eso es el soft landing ideal.

La exportación se hace únicamente como backup de seguridad.

---

# 18. Backup de Engram antes de migrar

Antes de tocar integración alguna:

```bash
engram doctor
```

Luego crear export:

```bash
engram export <archivo> --project <project>
```

o cuando corresponda:

```bash
engram export <archivo> --all
```

Guardar bajo un snapshot de GabitoTL:

```text
~/.gabitotl/migrations/
└── 2026-10-05_2100_gentle-ai/
    ├── migration.md
    ├── gentle-ai/
    ├── configs/
    └── memory/
        └── engram-export.json
```

Opcionalmente puede hacerse también una copia consistente del datastore de Engram cuando el runtime permita hacerlo de forma segura.

La exportación oficial es preferible como backup portable.

---

# 19. ¿Qué significa “migrar la memoria”?

Hay dos escenarios distintos.

## Escenario A — usuario sigue con Engram

No existe migración de datos.

Sólo existe:

```text
reutilización de memoria
+
reconfiguración del MCP
```

El resultado:

```text
antes:
Claude → Gentle-AI config → Engram

después:
Claude → GabitoTL config → mismo Engram
```

Todos los recuerdos anteriores siguen estando.

---

# 20. Escenario B — usuario pasa a memoria compartida

Ejemplo:

```text
antes:
Gentle-AI + Engram

después:
GabitoTL + Memory MCP + PostgreSQL
```

Acá sí existe migración.

Workflow:

```text
Engram
  │
  ▼
engram export JSON
  │
  ▼
Migration Adapter
  │
  ├── project identity
  ├── observations
  ├── decisions
  ├── prompts útiles
  ├── session summaries
  └── relations si el destino las soporta
  │
  ▼
Memory MCP
  │
  ▼
PostgreSQL
```

Pero debe existir una regla:

> No se debe asumir que cualquier Memory MCP tiene el mismo schema que Engram.

Crear una interfaz:

```text
MemoryMigrator
```

con implementaciones concretas:

```text
Engram → MemoryMCPProviderX
Engram → MemoryMCPProviderY
```

Hasta seleccionar el servidor Memory MCP definitivo, la V1 debe preservar el export de Engram y migrar únicamente cuando existe un adapter probado.

Nunca perder datos para “forzar” una migración.

---

# 21. Migración de documentación del proyecto

Gentle-AI puede haber dejado conocimiento fuera de Engram.

GabitoTL debe hacer un relevamiento antes de desinstalarlo.

Buscar:

```text
documentación gestionada por Gentle-AI
work artifacts
ODD task documents
instructions
skills seleccionados
agent-specific guidance
```

También mirar configuraciones que Gentle-AI agregó en:

```text
Claude Code
OpenCode
Kiro
```

No copiar todo.

Extraer únicamente conocimiento de proyecto.

Ejemplos:

```text
decisiones
convenciones
arquitectura documentada
estado de features
trabajo pendiente
comandos importantes
restricciones
```

Guardar el resultado en:

```text
.gabitotl/imports.md
```

y memoria.

---

# 22. Migration Report

Crear:

```text
.gabitotl/imports.md
```

Ejemplo:

```markdown
---
source: gentle-ai
imported_at: 2026-10-05T21:00:00-03:00
status: complete
---

# Gentle-AI

## Memoria

Engram detectado y reutilizado.

Proyecto Engram:
ecommerce

Observaciones encontradas:
1248

Backup:
~/.gabitotl/migrations/2026.../engram-export.json

## Configuración importada

- Convenciones del proyecto
- Contexto de autenticación
- Estado de tareas activas

## No importado

- Persona Gentleman
- Skills de Gentle-AI no seleccionados
- Hooks propios de Gentle-AI

## Retirado

- Configuración administrada por Gentle-AI en Claude Code
- Configuración administrada por Gentle-AI en OpenCode

## Preservado

- Engram
- Memoria
- archivos del usuario
- configuración no administrada por Gentle-AI
```

---

# 23. Desinstalación de Gentle-AI

Después de verificar GabitoTL:

```text
GabitoTL funcionando
✓ memoria
✓ Claude
✓ OpenCode
✓ Kiro
```

mostrar:

```text
Gentle-AI ya no es necesario para este proyecto.

¿Qué hacemos?

❯ Retirar configuración de Gentle-AI
  Mantenerlo instalado
  Revisar qué se va a quitar
```

Si el usuario elige retirarlo, preferir usar su propio mecanismo:

```bash
gentle-ai uninstall --all
```

o una desinstalación más acotada según los agentes detectados.

Esto tiene dos ventajas:

1. Gentle-AI conoce qué archivos administra.
2. Evitamos borrar manualmente configuración que no nos pertenece.

---

# 24. Qué NO debe borrar la migración

Nunca eliminar automáticamente:

```text
~/.engram/
.engram/
Engram binary
memorias
repositorios
branches
commits
configuración personal ajena a Gentle-AI
skills del usuario no administrados
secrets
credentials
```

Tampoco borrar inmediatamente:

```text
~/.gentle-ai/backups/
```

Los backups previos pueden ser útiles durante un período de transición.

---

# 25. Limpieza diferida

Después de una migración exitosa, recomendar mantener:

```text
~/.gentle-ai/
```

durante algunos días, o moverlo a:

```text
~/.gabitotl/legacy/gentle-ai/
```

No es necesario conservar todo para siempre.

Una política posible:

```text
keep legacy migration snapshot: 30 days
```

Luego:

```bash
gabitotl cleanup
```

puede ofrecer eliminar residuos ya innecesarios.

---

# 26. Snapshot global de migración

Antes de tocar nada:

```text
~/.gabitotl/migrations/<timestamp>/
```

Ejemplo:

```text
~/.gabitotl/migrations/
└── 2026-10-05_2100_gentle-ai/
    ├── migration.md
    ├── inventory.json
    │
    ├── memory/
    │   └── engram-export.json
    │
    ├── gentle-ai/
    │   └── state.json
    │
    ├── claude/
    │   └── ...
    │
    ├── opencode/
    │   └── ...
    │
    └── kiro/
        └── ...
```

Sólo copiar archivos relevantes.

No duplicar repositorios completos.

---

# 27. Rollback de instalación

Si algo falla:

```text
Error configurando OpenCode
        │
        ▼
detener instalación
        │
        ▼
revertir mutaciones GabitoTL
        │
        ▼
restaurar configs snapshot
        │
        ▼
Gentle-AI sigue intacto
        │
        ▼
Engram sigue intacto
```

Gentle-AI no debe retirarse hasta que todos los checks críticos pasen.

---

# 28. Flujo TUI de migración

Pantalla 1:

```text
Encontré una instalación anterior

Gentle-AI 4.x
✓ Claude Code
✓ OpenCode
✓ Kiro

También encontré Engram con memoria existente.

¿Qué querés hacer?

❯ Migrar todo a GabitoTL
  Instalar GabitoTL y mantener Gentle-AI
  Ver inventario
```

Pantalla 2:

```text
Tu memoria está segura

Engram:
✓ base encontrada
✓ integridad OK
✓ proyecto identificado
✓ backup exportable

GabitoTL puede reutilizarla sin copiarla.

❯ Usar esta memoria
  Configurar memoria compartida
```

Pantalla 3:

```text
Plan

✓ Crear backup de seguridad
✓ Crear .gabitotl/
✓ Importar documentación útil
✓ Configurar Gabito en Claude Code
✓ Configurar Gabito en OpenCode
✓ Configurar Gabito en Kiro CLI
✓ Configurar Gabito en Kiro IDE
✓ Verificar Engram

Después:

✓ Ofrecer retirar Gentle-AI

[ Aplicar ]
```

Pantalla final:

```text
Listo.

GabitoTL
✓ configurado

Memoria
✓ 1.248 recuerdos preservados

Runtimes
✓ Claude Code
✓ OpenCode
✓ Kiro CLI
✓ Kiro IDE

Gentle-AI
✓ configuración retirada
✓ backups conservados

TIP:
No borré los backups de Gentle-AI todavía.
Cuando estés seguro podés ejecutar:

gabitotl cleanup
```

---

# 29. Instalación cuando Gentle-AI no existe

Flujo:

```text
gabitotl
   │
   ▼
detectar proyecto
   │
   ▼
detectar runtimes
   │
   ▼
detectar Engram
   │
   ├── existe → ofrecer reutilizar
   │
   └── no → instalar/configurar si se eligió modo personal
   │
   ▼
crear .gabitotl
   │
   ▼
configurar runtimes
   │
   ▼
investigar proyecto
   │
   ▼
crear memoria inicial
```

---

# 30. Onboarding dentro del instalador

Preguntar sólo lo que no se puede detectar.

Ejemplo:

```text
Encontré:
✓ GitHub
✓ OpenCode
✓ Claude Code
✓ Kiro
✓ Engram

Necesito saber tres cosas:

¿Cómo querés que te llame?
> Gabi

¿Trabajás solo o con equipo?
> Solo

¿Querés que Gabito maneje commits y PR?
> Sí
```

No preguntar:

```text
¿qué runtime tenés?
¿usás Engram?
¿Git está instalado?
¿qué proveedor Git usás?
```

si ya puede detectarlo.

---

# 31. Memoria personal vs compartida

Pantalla:

```text
¿Cómo vas a trabajar?

❯ Yo solo
  Usa Engram local.

  En equipo
  Usa memoria compartida en PostgreSQL.
```

Si el usuario viene de Gentle-AI con Engram y elige equipo:

```text
Encontré memoria previa en Engram.

¿Qué hacemos?

❯ Importarla a la memoria compartida
  Mantener Engram como memoria histórica
  Empezar la memoria compartida desde cero
```

La opción recomendada debe ser:

```text
Importarla
```

sólo si existe un migrador probado para el Memory MCP elegido.

---

# 32. Configuración automática por runtime

El instalador debe tener:

```text
RuntimeAdapter
```

Cada adapter implementa algo conceptualmente parecido a:

```text
Detect()
Inspect()
Backup()
Plan()
Apply()
Verify()
RemoveGabitoConfig()
```

GabitoTL no debe tener un bloque central lleno de:

```text
if claude
if opencode
if kiro
```

---

# 33. Configuración de Claude Code

El adapter:

```text
detecta ~/.claude
detecta binary claude
lee configuración existente
preserva contenido del usuario
instala persona Gabito
configura Memory MCP / Engram
configura instrucciones GabitoTL
verifica
```

Debe marcar sus secciones para poder retirarlas después.

Nunca apropiarse de todo un archivo del usuario.

---

# 34. Configuración de OpenCode

El adapter:

```text
detecta binary
detecta XDG config
lee opencode.json/jsonc
detecta proyecto
preserva providers/modelos existentes
agrega únicamente configuración GabitoTL
configura memoria
verifica
```

No debe reemplazar selección de modelos salvo que el usuario lo elija.

---

# 35. Configuración de Kiro

Como CLI e IDE comparten `~/.kiro`, el adapter debe tener un core común.

```text
KiroAdapterCore
   │
   ├── KiroCLI
   └── KiroIDE
```

Configurar según capacidades:

```text
steering
MCP
skills si en el futuro se usan
config de proyecto
```

No duplicar entradas MCP.

---

# 36. Migración desde Kiro existente

Si encuentra:

```text
.kiro/steering/
.kiro/specs/
.kiro/agents/
.kiro/skills/
```

antes de configurar GabitoTL:

```text
leer
clasificar
importar conocimiento útil
registrar imports
```

No borrar Kiro.

Kiro es un runtime soportado, no un harness que necesariamente haya que retirar.

---

# 37. Detección de conflicto de personas

Ejemplo:

```text
Claude Code tiene:
- instrucciones propias
- Gentleman persona
- Gabito persona
```

Eso no debe ocurrir.

Durante migración:

```text
1. detectar ownership
2. retirar únicamente sección Gentleman administrada
3. preservar instrucciones del usuario
4. instalar sección Gabito
```

El resultado debe quedar limpio.

---

# 38. Ownership

Todo asset escrito por GabitoTL debe poder identificarse.

Ejemplo Markdown:

```text
<!-- gabitotl:start persona -->
...
<!-- gabitotl:end persona -->
```

Cuando el formato no permite comments, usar un registro interno de ownership:

```text
~/.gabitotl/state.json
```

Debe contener:

```text
runtime
path
managed keys
hash before
hash after
version
```

---

# 39. Estado global de GabitoTL

Además del `.gabitotl/` de cada proyecto, mantener:

```text
~/.gabitotl/
```

Ejemplo:

```text
~/.gabitotl/
├── state.json
├── installations/
├── migrations/
├── backups/
├── legacy/
└── logs/
```

El estado global sirve para instalación.

El estado de proyecto sigue viviendo en:

```text
<repo>/.gabitotl/
```

---

# 40. Dos niveles de backup

## Global

Para cambios de configuración de agentes:

```text
~/.gabitotl/backups/
```

Ejemplos:

```text
~/.claude/
~/.config/opencode/
~/.kiro/
```

pero sólo archivos tocados.

## Proyecto

Para código:

```text
<project>/.gabitotl/.bak/
```

Nunca mezclar ambos conceptos.

---

# 41. Doctor

`gabitotl doctor` debe ser read-only.

Ejemplo:

```text
GabitoTL doctor

Core
✓ GabitoTL 1.0

Proyecto
✓ .gabitotl

Memoria
✓ Engram
✓ proyecto ecommerce
✓ MCP responde

Runtimes
✓ Claude Code
✓ OpenCode
✓ Kiro CLI
✓ Kiro IDE

Git
✓ repository
✓ GitHub
✓ identity: Gabi

Legacy
! Gentle-AI backup disponible
```

---

# 42. `--dry-run`

Todo instalador serio debe soportar:

```bash
gabitotl install --dry-run
```

Salida:

```text
Plan:

CREATE
  .gabitotl/project.md

MODIFY
  ~/.claude/...
  ~/.config/opencode/...
  ~/.kiro/...

PRESERVE
  ~/.engram/engram.db

IMPORT
  Gentle-AI project knowledge

REMOVE AFTER VERIFY
  Gentle-AI managed persona
  Gentle-AI managed MCP entries

NO ACTION
  user custom skills
```

No ejecutar ningún cambio.

---

# 43. Modo no interactivo

Para CI, dotfiles o automatizaciones:

```bash
gabitotl install \
  --runtime claude-code \
  --runtime opencode \
  --memory engram \
  --user gabi \
  --yes
```

Migración:

```bash
gabitotl migrate gentle-ai \
  --preserve-engram \
  --remove-gentle-ai \
  --yes
```

Pero la opción destructiva:

```text
--remove-gentle-ai
```

sólo debe ejecutarse después de verificaciones exitosas.

---

# 44. Instalación del propio GabitoTL

Opciones futuras:

macOS/Linux:

```bash
brew install gabitotl/tap/gabitotl
```

Windows:

```powershell
winget install GabitoTL.GabitoTL
```

Instalador universal:

```bash
curl ... | sh
```

PowerShell:

```powershell
irm ... | iex
```

Para seguridad, preferir que estos scripts:

1. detecten plataforma;
2. descarguen release específico;
3. verifiquen checksum;
4. instalen solamente el binario;
5. ejecuten `gabitotl install`.

El script bootstrap debe ser mínimo.

La lógica real vive en el binario.

---

# 45. Upgrade

```bash
gabitotl update
gabitotl upgrade
```

Antes de cambiar configuración:

```text
backup
→ update binary
→ migration schema
→ dry reconciliation
→ apply
→ verify
```

No reconfigurar todos los runtimes ciegamente.

Sólo los registrados en:

```text
~/.gabitotl/state.json
```

---

# 46. Uninstall de GabitoTL

Debe seguir la misma filosofía:

```text
gabitotl uninstall
```

Mostrar:

```text
Se eliminará:

✓ Persona Gabito de Claude
✓ Config GabitoTL de OpenCode
✓ Config GabitoTL de Kiro
✓ archivos administrados globales

Se conservará:

✓ Engram
✓ Memory MCP / PostgreSQL
✓ memorias
✓ código
✓ .gabitotl del proyecto
✓ backups
```

El usuario puede optar después por eliminar proyecto/configuración/memoria por separado.

Nunca asumir que desinstalar el harness equivale a borrar conocimiento.

---

# 47. Reglas de seguridad de migración

1. No borrar memoria.
2. No modificar DB de Engram directamente.
3. Usar export/import oficial para migraciones.
4. Ejecutar `engram doctor` antes y después.
5. Crear snapshot antes de modificar configs.
6. No retirar Gentle-AI antes de verificar GabitoTL.
7. No eliminar configuración que no podamos atribuir a Gentle-AI.
8. No borrar skills personalizados.
9. No guardar secrets en reportes.
10. Toda migración debe poder revertirse.

---

# 48. Algoritmo conceptual del soft landing

```text
func MigrateGentleAI():

    detect GentleAI

    if not found:
        return

    inventory = inspectGentleAI()

    engram = detectEngram()

    if engram:
        verifyEngram()
        exportEngramBackup()

    snapshotConfigs(inventory)

    knowledge = extractProjectKnowledge(
        gentleAI,
        engram,
        runtimeConfigs,
        projectDocs,
    )

    createGabitoTLProject()

    importKnowledge(knowledge)

    configureSelectedRuntimes()

    reconnectMemory()

    if not verifyGabitoTL():
        rollback()
        stop

    askUserToRemoveGentleAI()

    if yes:
        useGentleAIManagedUninstall()

    verifyAgain()

    writeMigrationReport()
```

---

# 49. Diferencia entre instalar y adoptar

El instalador debe entender este concepto.

Si encuentra una herramienta existente:

```text
NO instalar otra
```

Debe **adoptarla**.

Ejemplo:

```text
Engram encontrado.

✓ Lo vamos a reutilizar.
No voy a instalar otra copia.
```

Lo mismo con:

```text
Claude Code
OpenCode
Kiro
Git
```

---

# 50. Estado visual simple

La TUI debería utilizar cuatro estados:

```text
✓ listo
! necesita atención
→ se va a configurar
× error
```

Evitar pantallas llenas de información técnica.

Ejemplo:

```text
Memoria
✓ Engram existente

Gentle-AI
! Se puede migrar

Claude Code
→ Se configurará

OpenCode
→ Se configurará
```

Detalles disponibles con:

```text
[ Ver detalles ]
```

---

# 51. Mensajes con personalidad Gabito

El instalador también debe respetar la personalidad.

No:

```text
MCP configuration reconciliation was successfully completed.
```

Sí:

```text
Listo. Engram sigue usando la misma memoria y ya quedó conectado a GabitoTL.
```

Ante migración:

```text
Ojo:
Encontré Gentle-AI y 1.248 recuerdos en Engram.
Primero voy a respaldar esa memoria; no la voy a tocar durante la desinstalación.
```

Al final:

```text
TIP:
Dejé el backup de Gentle-AI guardado por si necesitás volver atrás. Cuando estés seguro, lo podemos limpiar.
```

---

# 52. MVP del instalador

Primera versión:

- Go single binary.
- TUI.
- macOS/Linux/Windows.
- detección Claude Code.
- detección OpenCode.
- detección Kiro CLI.
- detección Kiro IDE.
- detección Gentle-AI.
- lectura `~/.gentle-ai/state.json`.
- detección Engram.
- `engram doctor`.
- `engram export`.
- creación `.gabitotl/`.
- snapshot global.
- adopción de Engram existente.
- configuración automática de runtimes.
- `--dry-run`.
- `doctor`.
- rollback.
- desinstalación Gentle-AI después de verificar.
- migration report.

Después:

- import automático Engram → Memory MCP/PostgreSQL.
- más harness importers.
- package manager installation automática.
- update/upgrade.
- cleanup de legacy.
- GitHub/Bitbucket authentication helpers.
- instalación de runtimes faltantes.

---

# 53. Caso ideal de migración

Antes:

```text
Gentle-AI
   │
   ├── Claude
   ├── OpenCode
   ├── Kiro
   │
   └──────► Engram
              │
              └─ años de memoria
```

Después:

```text
GabitoTL
   │
   ├── Claude
   ├── OpenCode
   ├── Kiro CLI
   ├── Kiro IDE
   │
   └──────► MISMO Engram
              │
              └─ misma memoria
```

Lo que cambia es el harness.

Lo que **no cambia** es el conocimiento acumulado.

---

# 54. Caso migración a equipo

Antes:

```text
Gentle-AI
     │
     ▼
   Engram
  personal
```

Soft landing:

```text
Engram
  │
  ├── export de seguridad
  │
  └── migrador probado
          │
          ▼
     Memory MCP
          │
          ▼
      PostgreSQL
```

Luego:

```text
                 PostgreSQL
                    ▲
                    │
               Memory MCP
                    ▲
             ┌──────┴──────┐
             │             │
          Gabi/Gabito    Gonza/Gabito
```

Las observaciones migradas deben conservar, cuando esté disponible:

```text
origen: engram
autor original
fecha
proyecto
tipo
contenido
relaciones
```

Si no se conoce autor histórico:

```text
actor: legacy
source: engram/gentle-ai
```

Nunca inventar quién hizo un cambio.

---

# 55. Una precaución importante

Engram actual ya posee mecanismos de export/import, Git sync e incluso opciones de sincronización remota.

Esto no cambia la decisión de GabitoTL de ofrecer:

```text
Engram = personal
Memory MCP/PostgreSQL = equipo
```

pero sí significa que el instalador no debe asumir que una instalación Engram es necesariamente “local simple”.

Antes de migrarla debe detectar:

```text
ENGRAM_DATA_DIR
cloud/autosync si existe
project identity
.engram project config
```

y mostrar un `Ojo` si encuentra una topología que no es trivial.

---

# 56. Referencias técnicas verificadas

Gentle-AI:

- Usage / uninstall:
  https://github.com/Gentleman-Programming/gentle-ai/blob/main/docs/usage.md

- Memory ownership:
  https://github.com/Gentleman-Programming/gentle-ai/blob/main/docs/codebase/memory-core.md

- Kiro adapter:
  https://github.com/Gentleman-Programming/gentle-ai/blob/main/docs/kiro.md

- Platform paths:
  https://github.com/Gentleman-Programming/gentle-ai/blob/main/docs/platforms.md

Engram:

- Technical reference:
  https://github.com/Gentleman-Programming/engram/blob/main/DOCS.md

- Installation:
  https://github.com/Gentleman-Programming/engram/blob/main/docs/INSTALLATION.md

Kiro:

- CLI configuration:
  https://kiro.dev/docs/cli/chat/configuration/

- CLI settings:
  https://kiro.dev/docs/cli/reference/settings/

OpenCode:

- Configuration:
  https://opencode.ai/v2/docs/config

---

# 57. Principio final del instalador

> **GabitoTL entra a una casa que ya está habitada: primero mira qué hay, guarda una copia de las cosas importantes, reutiliza lo que sirve y recién después cambia los muebles.**

Ese debe ser el comportamiento del instalador.

En especial:

```text
NO:
desinstalar Gentle-AI
→ instalar GabitoTL
→ descubrir que perdimos contexto

SÍ:
detectar
→ respaldar
→ adoptar memoria
→ importar conocimiento
→ instalar
→ verificar
→ retirar lo anterior
```

Esto convierte la instalación en una migración segura y no en un “borrón y cuenta nueva”.
