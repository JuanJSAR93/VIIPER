# Plan de implementación: transporte simple Xbox One/Series

## Objetivo

Hacer que Xbox One/Series tenga el mismo ciclo de vida operativo que Xbox 360, DS4, DualSense y NS2P:

```
crear → abrir stream → USB/IP attach → mantener → cerrar → retirar
```

Se conserva la identidad oficial GIP/XGIP, los reportes GIP y los cuatro
actuadores de feedback. El cambio se limita al ciclo de vida y al manejo de
errores del transporte.

## Alcance

Incluye:

- Transporte simple opcional para Xbox One y Xbox Series X|S.
- Una única sesión por `busId/devId`.
- Entrada y feedback coordinados.
- Errores de feedback aislados de la vida USB/IP.
- Neutral seguro ante pérdida temporal de ACK.
- Limpieza determinista de stream, attach y bus.
- Diagnóstico de conexión, ACK, TTL y desconexión.

No incluye:

- Cambiar VID/PID, descriptor GIP o identidad Xbox.
- Modificar Xbox 360, DS4, DualSense o NS2P.
- Eliminar inicialmente el broker persistente existente.
- Cambiar `CFBK v1`.
- Convertir GIP en HID/XUSB.

## Estado actual

Ya existen `RumbleBodyV1`, el ejecutor temporal, la renovación de TTL,
secuencias CFBK y ACK del broker. El plan debe reutilizarlos y no duplicarlos.

La primera fase del modo `simple` ya está implementada: el stream sigue siendo
full-duplex y autenticado, pero la entrega de feedback se considera confirmada
cuando llega al stream, sin depender del ACK externo del modo `broker`. Si el
feedback falla, se publica entrada neutral y se conserva la persona USB/IP.
El modo `broker` continúa siendo el predeterminado y conserva su política
fail-closed.

## Modos de transporte

```
xboxone-transport=broker   # modo actual, predeterminado inicialmente
xboxone-transport=simple   # nuevo modo compatible
```

El modo simple será opt-in durante la validación. El modo broker seguirá
disponible como fallback.

## Flujo simple

```
cliente autenticado
  → crear bus/persona Xbox
  → abrir sesión full-duplex
  → activar USB/IP
  → publicar entrada semántica
  → recibir Direct Motor GIP
  → entregar feedback al cliente
```

El parser GIP solo interpreta bytes. No gestiona sockets, TTL ni cierres.

## Contratos que no se modifican

### GIP

Se mantienen:

- Mensaje Direct Motor número 9.
- Cuerpo de 9 bytes.
- Niveles 0..100.
- `duration`, `delay` y `repeat`.
- Mapeo de motores y gatillos.

### CFBK

Si el modo simple usa CFBK, conserva el frame completo de 72 bytes:

```
BodyLow      → vibración izquierda
BodyHigh     → vibración derecha
LeftTrigger  → impulso izquierdo
RightTrigger → impulso derecho
```

No se sustituye por dos bytes de motor.

### API C/C++

No se cambia la firma de `XboxOneOutputCallback`.

## Máquina de estados

```
Created → StreamOpening → Attached → Active
                                      ├─ FeedbackPending
                                      ├─ FeedbackInFlight
                                      ├─ Neutralizing
                                      └─ Closing
Closed / Quarantined
```

Reglas:

- Un fallo temporal de feedback no cierra directamente USB/IP.
- ACK perdido produce Neutral.
- Neutral mantiene el mando conectado.
- Stop se reserva para desconexión definitiva.
- Una generación anterior nunca publica sobre una sesión nueva.
- Solo una rutina es propietaria del cierre final.

## Cola y ACK

La cola tendrá como máximo:

```
frame en vuelo: 0 o 1
frame pendiente: 0 o 1
```

Una orden nueva reemplaza el estado pendiente. No se acumulan vibraciones
obsoletas.

Secuencia:

1. Publicar un frame.
2. Esperar ACK o expiración.
3. Validar correlación y generación.
4. Liberar el frame en vuelo.
5. Publicar el último frame pendiente.

ACK duplicados o fuera de orden se registran e ignoran, salvo que indiquen una
sesión inválida.

## Temporización

- `duration` permanece en el ejecutor temporal.
- Las renovaciones usan una secuencia nueva.
- Nunca se extiende una orden más allá de su duración absoluta.
- `delay` se implementa localmente:

```
Neutral durante delay → Apply durante duration → repetición → Neutral final
```

Si `delay` aún no está implementado, debe rechazarse explícitamente.

## Limpieza USB/IP

La secuencia debe ser idempotente:

1. Detener nuevas entradas y feedback.
2. Cancelar renovadores.
3. Publicar Neutral si el canal sigue válido.
4. Cerrar el stream.
5. Retirar el attach actual.
6. Liberar el registro.
7. Eliminar el bus solo si pertenece a la sesión.

Nunca se reutiliza un `usbipBusId` antiguo. Cada `x1-*` es válido solo para
su registro actual.

## Observabilidad

Registrar por sesión:

- `busId`, `devId`, perfil y `usbipBusId`.
- Transiciones de estado y motivo de cierre.
- Secuencia, generación y comando CFBK.
- ACK aceptado, rechazado, duplicado o fuera de orden.
- Tiempo de publicación a ACK.
- Renovaciones.
- Errores de importación USB/IP.

## Fases

### Fase 1: línea base

- Prueba neutral durante 60 segundos.
- Vibración sostenida durante más de 250 ms.
- ACK perdido, duplicado y fuera de orden.
- Tres ciclos limpios sin buses previos.

### Fase 2: abstracción

- Crear una interfaz interna de abrir/mantener/cerrar.
- Adaptar el broker actual sin cambiar su semántica.
- Centralizar la limpieza.
- Mantener la API pública.

### Fase 3: modo simple

- Añadir sesión full-duplex.
- Reutilizar parser GIP, `RumbleBodyV1` y ejecutor temporal.
- Añadir cola de último estado.
- Aislar fallos de ACK del cierre USB/IP.

### Fase 4: temporización completa

- Implementar `delay`.
- Validar `duration` y `repeat`.
- Verificar límites absolutos y renovaciones.

### Fase 5: validación Windows

- `joy.cpl` durante 60 segundos.
- `XInputGetState` durante 60 segundos.
- Vibración izquierda, derecha y simultánea.
- Neutral, Stop, reconexión y pérdida de ACK.
- Verificar que no queden buses huérfanos.

### Fase 6: activación

- Mantener broker como valor predeterminado.
- Activar simple en el cliente de diagnóstico.
- Comparar estabilidad y latencia.
- Cambiar el valor predeterminado solo después de superar los criterios.

## Matriz mínima

| Prueba | Resultado |
|---|---|
| Neutral 60 s | Sin desconexión |
| Vibración izquierda/derecha | Canal correcto |
| Cuatro actuadores | Valores independientes |
| Vibración sostenida | Renovación sin caída |
| `duration=0` | Neutral inmediato |
| `delay>0` | Sin vibración prematura |
| ACK perdido | Neutral y mando conectado |
| ACK duplicado | Ignorado sin cierre |
| Reconexión | Nueva generación |
| Cierre normal | Sin bus huérfano |
| Xbox One/Series | Misma ruta, identidad distinta |

## Criterios de aceptación

- Diez ciclos consecutivos sin desconexión inesperada.
- Cinco minutos conectado.
- Vibración alternada durante 60 segundos.
- Ningún import de `usbipBusId` antiguo.
- `joy.cpl` y XInput mantienen el mando visible.
- Los otros mandos no cambian su comportamiento.
- El modo broker actual sigue pasando sus pruebas.

## Reversión

Si falla el modo simple:

1. Desactivar `xboxone-transport=simple`.
2. Volver a `broker`.
3. Conservar logs y `devId`.
4. No reutilizar generación ni `usbipBusId` fallidos.
5. Corregir el modo simple sin tocar otros dispositivos.

## Resultado esperado

Desde el punto de vista del usuario, Xbox One/Series se conecta, permanece
visible y se desconecta como los demás mandos. Internamente conserva GIP/XGIP,
pero un error temporal de feedback no elimina por sí solo el dispositivo
USB/IP.

