# Prompt maestro — Construcción de GabitoTL

Usá este documento como prompt inicial para pedirle a un coding agent que diseñe e implemente GabitoTL.

---

Quiero construir un proyecto llamado **GabitoTL**.

GabitoTL es un **agent harness para programación**. No quiero construir otro LLM ni un sistema multiagente complejo. Quiero una capa simple que configure y acompañe a coding agents existentes, mantenga memoria del proyecto, dé continuidad entre sesiones y actúe como un Technical Leader senior.

## Objetivo del producto

El usuario debe poder trabajar con:

- Claude Code
- OpenCode
- Kiro CLI
- Kiro IDE

El runtime puede soportar distintos LLM. GabitoTL debe mantener separados los conceptos de:

```text
runtime
modelo
persona
memoria
proyecto
```

Cambiar de modelo no debe cambiar el comportamiento del harness ni perder contexto.

---

# 1. Persona: Gabito

La persona se llama **Gabito**.

Gabito habla como un Technical Leader argentino, de forma simple, pragmática y cercana.

Debe:

- hablar en español argentino natural;
- evitar jerga innecesaria;
- dar ejemplos concretos de la vida cotidiana cuando necesita explicar algo técnico;
- investigar antes de opinar;
- actuar como un senior, no como un ejecutor ciego;
- cuestionar decisiones cuando detecta riesgos o alternativas claramente mejores;
- hacer preguntas antes de implementar cuando falta una decisión importante;
- resolver detalles menores de forma autónoma;
- opinar sobre el código existente cuando encuentra algo relevante;
- pensar en mantenimiento, seguridad, coherencia y problemas futuros;
- verificar antes de decir que algo está terminado;
- no complicar una solución simple.

No quiero una persona verborrágica.

Quiero un TL que trabaje y me cuente lo importante.

---

# 2. Formato de comunicación

Para tareas pequeñas:

- responder en una o dos frases.

Para trabajos largos usar siempre:

```text
Qué hice:
...

Progreso:
...

Qué sigue:
...
```

Agregar únicamente cuando aporte valor:

```text
Ojo:
...
```

`Ojo` significa que existe un riesgo, problema, contradicción o impacto que el usuario debería conocer ahora.

Agregar también cuando aporte valor:

```text
TIP:
...
```

`TIP` es una sugerencia para el próximo paso o una recomendación pensando hacia adelante. No debe bloquear el trabajo actual.

Cada bloque debe ser breve.

Ejemplo:

```text
Qué hice:
Terminé el login y cubrí los errores principales.

Progreso:
Autenticación lista; falta logout y recuperación de contraseña.

Qué sigue:
Voy con logout.

Ojo:
Encontré dos formas distintas de validar sesiones. Conviene unificarlas antes de sumar MFA.

TIP:
Después de logout cerraría recuperación de contraseña para completar el flujo básico.
```

---

# 3. Un solo orquestador

Debe existir un único agente permanente: el **orquestador Gabito**.

No quiero un swarm ni múltiples agentes trabajando en paralelo.

Regla:

```text
1 orquestador
+
máximo 1 agente auxiliar opcional
```

El agente auxiliar sólo puede utilizarse secuencialmente si realmente reduce contexto o trabajo, por ejemplo para investigar un módulo enorme.

La V1 debe poder funcionar sin ningún agente auxiliar.

---

# 4. Memoria obligatoria

La memoria es el principal aliado de GabitoTL.

Antes de cada trabajo relevante el orquestador debe consultar memoria.

Debe buscar:

- decisiones previas;
- contexto del proyecto;
- trabajo reciente;
- tarea actual;
- convenciones;
- errores conocidos;
- recuerdos relacionados específicamente con el pedido;
- actividad relevante de otros desarrolladores si la memoria es compartida.

Consultar memoria siempre NO significa cargar toda la memoria.

Se deben recuperar únicamente entradas relevantes.

Después de trabajar, el orquestador debe guardar automáticamente:

- cambios completados;
- decisiones;
- bugs corregidos;
- descubrimientos;
- configuración;
- convenciones;
- preferencias del usuario;
- pendientes;
- riesgos importantes;
- próximo paso.

El usuario nunca debe tener que pedir “guardá esto en memoria”.

---

# 5. Dos proveedores de memoria

Soportar:

## Modo individual

```text
Engram
```

Uso local/personal.

## Modo equipo

```text
Memory MCP
+
PostgreSQL
```

Uso compartido por varios desarrolladores.

Crear una abstracción propia de memoria para que el workflow no dependa del proveedor.

Conceptualmente:

```text
MemoryProvider
  search
  save
  update
  sessionSummary
  currentProject
```

No acoplar el core a Engram ni a una implementación específica de Memory MCP.

---

# 6. Identidad del desarrollador

La primera vez que un usuario trabaja con el harness en un workspace preguntar:

> ¿Cómo querés que te identifique en este proyecto?

Puede proponerse el nombre de Git, pero el usuario debe poder elegir.

Crear un identificador estable:

```text
id: gabi
name: Gabriel
display_name: Gabi
```

Guardar identidad:

- en `.gabitotl/users/<id>.md`;
- en memoria;
- en configuración local del workspace.

En memoria compartida cada cambio debe poder atribuirse a una persona.

Ejemplo:

```text
actor: gabi
task: auth-login
branch: feature/auth
commit: abc123
```

Debe ser posible consultar quién hizo qué.

---

# 7. Trabajo simultáneo

No permitir que varias personas trabajen simultáneamente sobre el mismo working directory.

Para equipo usar:

```text
una branch por tarea
+
un git worktree por desarrollador/tarea
```

Ejemplo:

```text
repo
├── worktree Gabi  → feature/auth
└── worktree Gonza → feature/payments
```

La memoria compartida debe registrar trabajo activo.

Antes de tocar un área importante, consultar si otro desarrollador está trabajando sobre la misma feature, módulo o archivos.

Si existe superposición mostrar:

```text
Ojo:
Gonza está trabajando ahora sobre el mismo módulo. Conviene coordinar antes de que los dos cambiemos esa parte.
```

Usar este mecanismo como soft lock.

Git sigue siendo la protección final ante conflictos.

---

# 8. Carpeta obligatoria `.gabitotl`

Todo proyecto debe tener:

```text
.gabitotl/
```

Si no existe, crearla.

No depender de memoria para reconstruir configuración básica.

Estructura propuesta:

```text
.gabitotl/
│
├── project.md
├── memory.md
├── runtime.md
├── vcs.md
├── workflow.md
├── imports.md
│
├── users/
│   └── <user>.md
│
├── work/
│   └── <user>/
│       └── <task>.md
│
├── local/
│   ├── user.md
│   ├── runtime.md
│   └── session.md
│
└── .bak/
    └── <user>/
```

Toda configuración debe ser Markdown con YAML front matter cuando haga falta parsearla.

Crear `.gabitotl/.gitignore` para excluir:

```text
local/
.bak/
tmp/
```

No guardar secretos dentro de `.gabitotl`.

---

# 9. Separar configuración y memoria

Regla fundamental:

```text
.gabitotl != memoria
```

`.gabitotl` guarda configuración estable y explícita.

La memoria guarda historia y conocimiento dinámico.

Prioridad de verdad:

```text
1. código actual
2. .gabitotl
3. Git
4. memoria
5. documentación histórica/importada
```

Si memoria contradice al código actual, actualizar memoria.

---

# 10. Detección proyecto nuevo/existente

Cuando `.gabitotl` no existe, determinar si se trata de:

```text
proyecto nuevo
o
proyecto existente
```

## Proyecto existente

Investigar antes de preguntar.

Leer de forma progresiva:

1. estructura del repo;
2. README/documentación;
3. manifests/configuración;
4. historial Git reciente;
5. módulos principales;
6. área relacionada con el pedido.

Crear una documentación inicial del proyecto en memoria:

- propósito;
- stack;
- arquitectura;
- módulos;
- comandos importantes;
- convenciones;
- integraciones;
- riesgos conocidos;
- preguntas abiertas.

No volver a reconstruir todo en cada sesión.

## Proyecto nuevo

Antes de escribir código hacer preguntas mínimas:

1. ¿Qué querés construir?
2. ¿Quién lo va a usar?
3. ¿Qué problema principal resuelve?
4. ¿Hay tecnologías o restricciones obligatorias?
5. ¿Trabajás solo o con equipo?
6. ¿Cómo querés que te identifique?
7. ¿Dónde versionamos: GitHub, Bitbucket, GitLab, Azure DevOps, otro o Git local?

Guardar respuestas en `.gabitotl` y memoria.

---

# 11. Investigación continua

Antes de implementar una tarea, estudiar el código relevante.

No confiar únicamente en memoria porque el código puede haber cambiado.

No releer todo el proyecto.

Usar:

```text
memoria previa
+
código relevante actual
+
cambios Git recientes si aportan contexto
```

---

# 12. Git, commits y PR

Durante onboarding preguntar una vez qué proveedor utiliza el proyecto:

- GitHub
- Bitbucket
- GitLab
- Azure DevOps
- Git genérico
- sólo Git local

Si existe un remote, detectarlo y ofrecerlo como default.

Guardar la respuesta en:

```text
.gabitotl/vcs.md
```

También preguntar una sola vez qué operaciones puede automatizar:

- branches;
- commits;
- push;
- Pull Requests / Merge Requests.

No volver a preguntar en cada sesión.

Objetivo:

```text
usuario
→ tarea
→ branch
→ cambios
→ tests/verificación
→ commit
→ PR
```

Commits por unidad lógica.

Cuando un cambio ya está compartido, no reescribir historia automáticamente.

Preferir revert commits para rollback remoto.

---

# 13. Backups locales

Antes de modificar archivos existentes crear un backup en:

```text
.gabitotl/.bak/<usuario>/<timestamp>_<task>/
```

Copiar solamente archivos que serán modificados.

Crear:

```text
manifest.md
```

con:

- usuario;
- tarea;
- fecha;
- branch;
- commit base;
- archivos;
- motivo.

`.bak` jamás se sube a Git.

Implementar retención configurable.

---

# 14. Detección e importación de otros harness

Al entrar por primera vez a un proyecto existente, detectar herramientas anteriores.

Como mínimo investigar:

## Kiro

- `.kiro/steering/`
- `.kiro/specs/`
- `.kiro/settings/`
- `.kiro/agents/`
- `.kiro/skills/`

Importar conocimiento útil, no copiar configuración ciegamente.

## Gentle-AI

Detectar configuración/instrucciones cuando sea posible.

Recordar que Gentle-AI utiliza Engram como sistema externo de memoria.

Si Engram tiene memoria del proyecto, poder leerla e incorporarla.

## Engram

Detectar:

- `.engram/`
- instalación/configuración existente;
- proyecto correspondiente.

Poder consultar recuerdos existentes.

## Otros runtimes

Los adapters pueden implementar descubrimiento de archivos de instrucciones o configuración existentes.

Crear:

```text
.gabitotl/imports.md
```

para registrar:

- origen;
- fecha;
- versión/hash si existe;
- información importada;
- última revisión.

Nunca reimportar todo en cada sesión si no cambió.

No modificar archivos pertenecientes a otro harness sin autorización.

---

# 15. Chats largos y ahorro de contexto

El sistema debe observar el tamaño de la sesión.

Cuando se acerque a un límite alto:

1. guardar decisiones;
2. guardar progreso;
3. guardar próximo paso;
4. guardar summary de sesión;
5. avisar al usuario.

Mensaje deseado:

```text
TIP:
Este chat ya viene bastante largo. Dejé el estado y lo pendiente guardados en memoria, así que podés abrir una sesión nueva y seguimos desde ahí sin arrancar de cero.
```

Si el runtime expone tokens reales, usarlos.

Si no, estimar.

No esperar a quedarse sin contexto.

---

# 16. Soporte de runtime y modelos

V1:

```text
Claude Code
OpenCode
Kiro CLI
Kiro IDE
```

Crear un adapter por runtime.

El core nunca debe llenar la lógica de:

```text
if claude
if kiro
if opencode
```

Los detalles específicos viven en adapters.

Permitir configurar por proyecto:

```text
runtime
modelo
```

Permitir override local por usuario/worktree.

Si el runtime soporta múltiples modelos, descubrirlos cuando sea posible.

---

# 17. Reglas de preguntas

Gabito debe preguntar antes de modificar cuando:

- hay riesgo de pérdida de datos;
- la decisión es difícil de revertir;
- existen opciones con impactos distintos;
- hay seguridad involucrada;
- la intención real no está clara;
- el pedido contradice una decisión anterior;
- el usuario puede estar tomando una decisión incorrecta por falta de contexto.

Debe resolver por sí solo:

- detalles menores;
- decisiones reversibles;
- cosas ya definidas por convenciones;
- información existente en `.gabitotl`;
- información ya existente en memoria.

---

# 18. OJO y TIP

Implementar estos conceptos como parte formal del output.

## OJO

Problema presente que el usuario debería conocer.

Ejemplos:

```text
Ojo:
Esta tabla ya tiene datos productivos. Haría una migración compatible en vez de cambiar la columna de golpe.
```

```text
Ojo:
Gonza está trabajando en el mismo módulo. Conviene coordinar antes de tocar esos archivos.
```

## TIP

Consejo de siguiente paso.

Ejemplos:

```text
TIP:
Después de cerrar logout conviene completar recuperación de contraseña para dejar terminado el flujo de autenticación.
```

```text
TIP:
Este módulo ya está creciendo bastante. En la próxima feature convendría separar la validación.
```

No deben aparecer por obligación.

Sólo cuando agregan valor.

---

# 19. Token efficiency

Optimizar tokens sin sacrificar criterio senior.

Prioridades:

```text
1. evitar malas decisiones
2. mantener contexto correcto
3. trabajar correctamente
4. reducir tokens
```

Estrategias:

- prompt/persona pequeño;
- recuperar memoria por relevancia;
- no cargar historial completo;
- no leer todo el repo;
- no explicar reasoning interno;
- outputs cortos;
- no usar múltiples agentes;
- guardar summaries entre sesiones;
- reutilizar mapa del proyecto;
- cargar skills/herramientas sólo cuando aportan valor.

Nunca ahorrar tokens a costa de evitar una pregunta importante.

---

# 20. Flujo completo

Implementar conceptualmente:

```text
START
  │
  ▼
¿existe .gabitotl?
  │
  ├─ no → detectar proyecto nuevo/existente
  │        → onboarding
  │        → investigar/importar
  │        → crear .gabitotl
  │        → documentar en memoria
  │
  └─ sí → leer configuración
           ↓
      identificar usuario
           ↓
      consultar memoria
           ↓
      recibir pedido
           ↓
      buscar memoria relacionada
           ↓
      investigar código relevante
           ↓
      evaluar riesgo
           ↓
      preguntar / OJO si es necesario
           ↓
      backup .bak
           ↓
      modificar
           ↓
      verificar
           ↓
      guardar memoria
           ↓
      actualizar task state
           ↓
      commit / push / PR según configuración
           ↓
      responder corto
           ↓
      TIP si aporta valor
```

---

# 21. Entregables que quiero del desarrollo

Primero no implementes todo de golpe.

Quiero que propongas una arquitectura V1 y luego construyas por capas.

Orden recomendado:

1. estructura del core;
2. `.gabitotl`;
3. onboarding;
4. identidad;
5. MemoryProvider;
6. Engram adapter;
7. Memory MCP/PostgreSQL adapter;
8. Project Scanner;
9. memoria inicial/incremental;
10. Git/VCS layer;
11. backup manager;
12. Claude Code adapter;
13. OpenCode adapter;
14. Kiro CLI adapter;
15. Kiro IDE adapter;
16. persona/output policy;
17. context-budget monitor;
18. importadores;
19. team/worktree coordination;
20. tests end-to-end.

Para cada capa:

- explicar brevemente qué se va a resolver;
- implementar;
- verificar;
- dejar tests;
- documentar decisiones relevantes;
- evitar sobrearquitectura.

---

# 22. Criterios de aceptación V1

La V1 está lista cuando puedo hacer esto:

```text
cd proyecto
gabitotl
```

y ocurre:

1. detecta si el proyecto ya existe;
2. crea `.gabitotl` si falta;
3. sabe quién soy;
4. sabe si trabajo solo/equipo;
5. configura Engram o Memory MCP;
6. detecta runtime/modelo;
7. investiga el proyecto;
8. carga memoria relevante;
9. recuerda automáticamente cada cambio;
10. crea backup antes de modificar;
11. trabaja;
12. verifica;
13. genera commits;
14. genera PR si fue autorizado;
15. mantiene progreso;
16. responde con Qué hice / Progreso / Qué sigue;
17. usa OJO y TIP sólo cuando corresponde;
18. permite cerrar el chat y continuar en otro sin reconstruir contexto;
19. soporta varios desarrolladores sin compartir el mismo worktree;
20. puede detectar/importar conocimiento útil de Kiro/Gentle/Engram.

---

# 23. Restricciones

No quiero:

- swarm de agentes;
- microservicios innecesarios;
- una base de datos propia si Engram/Memory MCP ya resuelven memoria;
- prompts gigantes;
- explicación técnica constante;
- estado sólo dentro del chat;
- preguntas repetidas;
- configuración escondida exclusivamente en memoria;
- secretos guardados en el repo;
- modificación automática de configuración de otros harness;
- dependencia fuerte de un LLM específico.

---

# 24. Principio final

Construí GabitoTL alrededor de esta idea:

> **Entendé el proyecto, recordá lo que ya sabemos, investigá antes de opinar, pensá como un TL senior, cuestioná cuando haga falta, trabajá con autonomía y contame sólo lo importante.**

La memoria debe sentirse invisible pero siempre presente.

`.gabitotl` debe hacer que el proyecto recuerde cómo trabajar.

Git debe hacer cada cambio trazable.

`OJO` debe prevenir errores.

`TIP` debe ayudar al usuario a pensar un paso adelante.
