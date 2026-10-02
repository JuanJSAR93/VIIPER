# Plan de reescritura Xbox One/Series sobre USB/IP

## Objetivo

Rehacer la implementación de Xbox One/Series para que funcione como una extensión normal de VIIPER, igual que Xbox 360, DualShock 4, DualSense y Switch 2 Pro.

La nueva implementación conservará USB/IP como transporte y añadirá dos perfiles independientes:

| Perfil | VID:PID | Identidad visible |
|---|---:|---|
| `xbox-one-original` | `045E:02D1` | Xbox One Controller |
| `xbox-series-xs` | `045E:0B12` | Xbox Series X\|S Controller |

La referencia de HIDMaestro se utilizará para estudiar descriptores, reportes, identidad y ciclo de vida, pero no se incorporará su driver UMDF2 ni se sustituirá el modelo USB/IP de VIIPER.

## Estado actual tras la implementación

La reescritura ya creó dos dispositivos nativos independientes siguiendo los
patrones de `device/xbox360`, `device/dualshock4`, `device/dualsense` y
`device/ns2pro`:

La reconstrucción empezará con dos paquetes independientes, equivalentes a los
demás dispositivos nativos:

```text
device/xboxone/
    const.go
    state.go
    descriptor.go
    device.go
    handler.go
    xboxone_test.go

device/xboxseries/
    const.go
    state.go
    descriptor.go
    device.go
    handler.go
    xboxseries_test.go
```

Cada paquete tendrá un único perfil físico y una identidad propia. No habrá un
`profile string` que cambie internamente entre dos mandos ni un catálogo
auxiliar para decidir qué implementación crear.

No se conserva ningún motor GIP retenido ni una ruta de broker especial. Las
APIs históricas de Xbox One fueron retiradas para que estos perfiles utilicen
exclusivamente el registro y stream genéricos de VIIPER.

La frontera de cada paquete será la misma que en los dispositivos nativos:
constructor `New`, descriptor USB, estado semántico, handler de stream,
callbacks de salida y pruebas de integración USB/IP.

## Patrones de los dispositivos existentes que se deben seguir

### Xbox 360

Se usará como referencia principal para la forma de un dispositivo Xbox en
VIIPER:

- `New(*device.CreateOptions)` como constructor.
- `usb.Device` como interfaz de transporte.
- `usb.Descriptor` propio del dispositivo.
- `api.RegisterDevice("xbox360", ...)` desde `handler.go`.
- Stream de entrada con un productor exclusivo.
- Callback de vibración hacia el cliente.
- Scheduler de reportes con estado neutral y manejo de overflow.
- Pruebas reales de descriptor, entrada y salida sobre USB/IP.

### DualShock 4 y DualSense

Se tomarán como referencia para:

- Separar `descriptor.go`, `device.go`, `handler.go` y `state.go`.
- Mantener el estado semántico independiente del reporte físico.
- Registrar callbacks de salida con generación/propietario.
- Reproducir el último estado de salida al conectar el callback.
- Liberar callbacks de forma condicional para no borrar el callback de otra
  conexión.
- Manejar seriales y múltiples instancias.
- Mantener el transporte de entrada y salida en el mismo stream.

### Switch 2 Pro

Se tomará como referencia para:

- Descriptores en un archivo dedicado.
- Estados de runtime y metadata separados.
- Reportes HID secuenciados.
- Handshake/control HID cuando Windows o el consumidor lo solicite.
- Pruebas de `joy.cpl`, reportes y protocolo de salida.

## Qué se toma de HIDMaestro

HIDMaestro será únicamente una guía de conformidad Windows:

- Descriptor HID Xbox One original.
- Descriptor HID Xbox Series USB.
- Orden de botones y ejes.
- Triggers separados.
- Reporte Xbox/GIP de entrada.
- Perfil de producto y fabricante.
- Reglas para conservar identidad, serial y ContainerId cuando sea posible.
- Pruebas contra XInput, DirectInput y GameInput.

No se copiarán su SDK, su driver UMDF2, `SwDeviceCreate`, su companion XUSB
ni su memoria compartida. La ruta de VIIPER seguirá siendo:

```text
cliente VIIPER
    -> stream genérico del dispositivo
    -> USB/IP server
    -> usbip-win2 / VHCI
    -> HIDClass / APIs de Windows
```

## Identidad visible de VIIPER

Al igual que los dispositivos existentes, la persona tendrá strings visibles
propias de VIIPER. El descriptor no intentará ocultar que es virtual:

```text
Manufacturer: ©Microsoft Corporation
Xbox One:     VIIPER Xbox One Controller
Series:       VIIPER Xbox Series X|S Controller
```

La metadata interna y el reporte de pruebas conservarán además:

```text
backend: usbip
profile: xbox-one-original | xbox-series-xs
virtual: true
source: viiper
```

La detección recomendada será por la combinación de:

- `Product`.
- `BusReportedDeviceDesc`.
- Fabricante.
- Propiedades PnP.
- Padre y servicio, cuando existan.

VID/PID no será la única señal. El producto visible no debe ser únicamente
`Controller`, porque perderíamos la diferenciación que ya existe en Xbox 360,
DS4, DS5 y NS2P.

## Dos dispositivos nativos, no un catálogo HID auxiliar

La integración se hará dentro de cada paquete mediante `handler.go`, igual que
Xbox 360, DS4, DualSense y NS2P. Cada paquete registrará su propio tipo desde
`init()`:

```text
xboxone
xboxseries
```

No se utiliza `internal/devicecatalog/xboxonehid.go`. Fue eliminado; el
catálogo importa directamente `device/xboxone` y `device/xboxseries`, cuyos
`handler.go` se registran por separado.

Cada paquete tendrá su propio handler:

```go
// device/xboxone/handler.go
func init() {
    api.RegisterDevice("xboxone", &handler{})
}

func (h *handler) CreateDevice(o *device.CreateOptions) (usb.Device, error)
func (h *handler) StreamHandler() api.StreamHandlerFunc
func (h *handler) UpdateMetaState(meta string, dev *usb.Device) error
```

```go
// device/xboxseries/handler.go
func init() {
    api.RegisterDevice("xboxseries", &handler{})
}
```

El código de empaquetado puede compartir utilidades internas si resulta
necesario, pero las decisiones de identidad, descriptor y estado permanecerán
en el paquete correspondiente. El objetivo es que cada mando sea extensible y
testeable como un dispositivo normal de VIIPER.

## Correspondencia de archivos

La librería C/Go de VIIPER también tendrá una entrada específica por mando:

```text
lib/viiper/xboxone.go
lib/viiper/xboxseries.go
```

Cada archivo expondrá el constructor y callback del mando correspondiente,
siguiendo el patrón de `xbox360.go`, `dualshock4.go`, `dualsense.go` y
`ns2pro.go`.

No habrá un único `lib/viiper/xboxone.go` con un parámetro genérico para
seleccionar Series. La separación facilita:

- ABI y nombres de tipos claros.
- Documentación independiente.
- Reportes separados.
- Pruebas separadas.
- Evolución independiente del descriptor.
- Compatibilidad futura con revisiones de hardware.

## Estado y stream unificados

El nuevo dispositivo utilizará un único estado semántico común:

```go
type InputState struct {
    Buttons      uint16
    LeftTrigger  uint16
    RightTrigger uint16
    LeftStickX   int16
    LeftStickY   int16
    RightStickX  int16
    RightStickY  int16
}
```

La conversión será:

```text
InputState
    -> Xbox One/Series HID input report
    -> USB/IP interrupt IN
```

La salida seguirá el patrón Xbox 360/DS4:

```text
USB/IP interrupt OUT
    -> decoder de vibración
    -> OutputState
    -> stream del cliente
```

No habrá un broker especial ni un cliente ejecutable separado.

## Secuencia de implementación completada

1. Crear `device/xboxone` y `device/xboxseries` como paquetes independientes.
2. Crear un descriptor HID dedicado para cada paquete.
3. Añadir `045E:02D1` al paquete Xbox One y `045E:0B12` al paquete Xbox Series.
4. Crear `device.go` en ambos paquetes con constructor, estado neutral y
   scheduler.
5. Crear `handler.go` en ambos paquetes y registrar `xboxone` y `xboxseries`.
6. Crear los encoders de reportes de entrada basados en la guía de HIDMaestro.
7. Crear los decoders de salida para motores izquierdo y derecho.
8. Implementar GET_DESCRIPTOR, GET_REPORT y SET_REPORT según lo requiera
    usbip-win2/HIDClass.
9. Añadir las identidades `VIIPER Xbox One Controller` y
    `VIIPER Xbox Series X|S Controller`.
10. Eliminar `internal/devicecatalog/xboxonehid.go` y registrar los dos
    paquetes desde `devices.go`.
11. Añadir las superficies de librería en `lib/viiper/xboxone.go` y
    `lib/viiper/xboxseries.go`.
12. Retirar la ruta GIP retenida, el cliente Xbox One antiguo y sus endpoints
    de autorización especiales.
13. Añadir pruebas unitarias de identidad, estado HID, entrada y vibración.
14. Compilar el binario principal y ejecutar la suite completa.
15. Compilar y probar Xbox Series X/S.
16. Validar joy.cpl, DirectInput, vibración y reconexión por separado.

## Criterio de migración de los imports actuales

Los imports actuales de `device/xboxone` se clasificarán así:

| Área | Acción |
|---|---|
| `internal/devicecatalog/xboxonehid.go` | Eliminado; ya no hay catálogo Xbox auxiliar |
| `device/xboxone` | Dispositivo nativo Xbox One implementado |
| `device/xboxseries` | Dispositivo nativo Xbox Series implementado |
| `lib/viiper/xboxone.go` | API nativa Xbox One HID/USB/IP |
| `lib/viiper/xboxseries.go` | API nativa Xbox Series implementada |
| `internal/server/api` | Stream y creación genéricos; sin endpoints Xbox especiales |
| `internal/registry` | Registro genérico; sin fábrica Xbox retenida |
| pruebas GIP/retained Xbox | Eliminadas por pertenecer al transporte retirado |
| scripts Python antiguos | Reescribir para los nuevos tipos genéricos |

No se mantiene un cliente ejecutable paralelo. La ruta pública nueva tiene la
misma forma que los otros mandos de `device/`.

## Convención de identidad VIIPER

Los demás mandos virtuales de VIIPER ya exponen una identidad visible propia
en sus strings USB. No ocultan el origen virtual cambiando únicamente el VID o
PID. Por ejemplo, las personas existentes utilizan productos como:

```text
VIIPER Xbox 360 Controller
VIIPER DualShock 4 Controller
VIIPER DualSense Wireless Controller
VIIPER Switch 2 Pro Controller
```

La reescritura Xbox seguirá la misma convención. Los valores iniciales serán:

```text
Manufacturer: Microsoft
Product One:  VIIPER Xbox One Controller
Product Series: VIIPER Xbox Series X|S Controller
```

Así se conservan VID/PID, descriptor y comportamiento esperado por Windows,
pero la detección de VIIPER puede realizarse de forma clara mediante el
producto, `BusReportedDeviceDesc` y las propiedades PnP relacionadas. No se
usará únicamente VID/PID para distinguir un mando físico de uno virtual.

El texto `VIIPER` debe estar en el descriptor de strings USB y, cuando Windows
lo materialice, debe quedar reflejado en las propiedades de dispositivo. La
implementación debe añadir pruebas que comprueben tanto `Product` como
`BusReportedDeviceDesc` después del attach real mediante usbip-win2.

## Resultado esperado

Cada perfil debe poder:

- Crearse desde la API y desde la librería VIIPER.
- Publicarse por USB/IP como un dispositivo USB independiente.
- Aparecer en Device Manager y `joy.cpl`.
- Exponer botones, d-pad, sticks y triggers.
- Ser identificado con el VID/PID y descriptor correspondientes.
- Recibir vibración sin desconectar el dispositivo.
- Eliminarse sin dejar dispositivos USB/IP huérfanos.
- Ejecutarse varias veces y liberar correctamente su bus, puerto y recursos.

La compatibilidad XInput se validará, pero no se asumirá automáticamente. USB/IP crea una persona USB; no reproduce por sí solo una conexión Bluetooth ni garantiza que Windows aplique `xinputhid.sys`.

## Perfiles iniciales

### Xbox One original

```yaml
id: xbox-one-original
vid: 045E
pid: 02D1
manufacturer: Microsoft
product: Xbox One Controller
connection: usb
trigger_mode: separate
buttons: 10
driver_mode: xinputhid-compatible
```

### Xbox Series X/S USB

```yaml
id: xbox-series-xs
vid: 045E
pid: 0B12
manufacturer: Microsoft
product: Xbox Series X|S Controller
connection: usb
trigger_mode: separate
buttons: 12
driver_mode: xinputhid-compatible
```

El perfil Series debe incluir el botón Share. El descriptor de entrada debe reflejarlo de forma consistente y no añadir el botón únicamente en la metadata.

## Qué se reutiliza de HIDMaestro

Se tomarán como referencia:

1. El descriptor HID Xbox de 262 bytes.
2. La separación de los triggers izquierdo y derecho.
3. El orden de botones y ejes utilizado por Windows.
4. La diferencia entre descriptor nativo y descriptor extendido del Series.
5. La conservación del último estado válido cuando no haya una actualización nueva.
6. La separación entre entrada, salida y ciclo de vida.
7. Las pruebas de identidad, recreación y limpieza.

No se copiarán directamente:

- `SwDeviceCreate`.
- Driver UMDF2.
- Dispositivo XUSB compañero.
- Memoria compartida de HIDMaestro.
- Instalador o certificado de HIDMaestro.
- Su SDK C#.

Esos elementos pertenecen a una arquitectura diferente y no deben mezclarse con la ruta USB/IP de VIIPER.

## Arquitectura nueva en VIIPER

```text
Aplicación VIIPER
       |
       v
lib/viiper/xboxone.go       lib/viiper/xboxseries.go
       |
       +-----------------------------+
       |                             |
       v                             v
device/xboxone                  device/xboxseries
       |
       +--> Descriptor USB/HID      +--> Descriptor USB/HID
       +--> Input/Output             +--> Input/Output
       +--> Metadata                 +--> Metadata
       |
       v
USB/IP server
       |
       v
usbip-win2 / VHCI
       |
       v
Windows HID / XInput / DirectInput
```

La persona Xbox debe comportarse como las demás personas de VIIPER: crear una especificación de dispositivo, registrarla en el bus virtual y publicar reportes a través del canal unificado.

## Fase 1: limpieza y reconstrucción desde cero

Se retiró el comando monolítico `viiper.exe xboxone-client`, junto con
`internal/cmd/xboxone_client.go` y su registro en `internal/config/config.go`.
Los scripts de prueba nuevos crean `xboxone` o `xboxseries` mediante la API
genérica y abren el stream normal `bus/{busId}/{devId}`.

La nueva implementación no tendrá una opción `legacy` ni un cliente paralelo.
Los nombres de perfil serán:

```text
device type: xboxone
device type: xboxseries
```

La reconstrucción se hará siguiendo los dispositivos nativos existentes y se
mantendrán intactos Xbox 360, DS4, DS5 y NS2P.

## Fase 2: modelo declarativo de perfiles

Crear una definición común para los dos perfiles:

```go
type XboxUSBProfile struct {
    ID                 string
    VendorID           uint16
    ProductID          uint16
    Manufacturer       string
    Product             string
    DeviceClass         byte
    DeviceSubClass      byte
    DeviceProtocol      byte
    Configuration       []byte
    HIDDescriptor       []byte
    InputReportSize     int
    OutputReportSize    int
    ButtonCount         int
    HasShareButton      bool
    TriggerMode         TriggerMode
}
```

El código de empaquetado será común. Las diferencias entre One y Series estarán en el perfil, el descriptor y el mapeo de botones.

## Fase 3: descriptores USB e HID

Implementar y probar por separado:

1. Descriptor de dispositivo USB.
2. Descriptor de configuración.
3. Descriptor de interfaz HID.
4. Descriptor HID.
5. Strings de fabricante, producto y número de serie.
6. Tamaño real de los reportes.

No se cambiarán VID/PID después de publicar el dispositivo. La identidad debe estar definida antes de que usbip-win2 lo importe.

Validaciones:

```text
VID/PID correctos
Manufacturer correcto
Product correcto
Número de interfaces correcto
Tamaño de input report correcto
Tamaño de output report correcto
Descriptor HID aceptado por HidClass
```

## Fase 4: empaquetado de entrada

Crear un encoder común:

```text
XboxState
   |
   v
Xbox USB/HID input report
```

Debe cubrir:

- D-pad.
- A, B, X, Y.
- LB y RB.
- View/Menu.
- Guide.
- LS y RS.
- Share en Series.
- Stick izquierdo.
- Stick derecho.
- Trigger izquierdo.
- Trigger derecho.

Los valores analógicos deben conservar resolución y signo según el descriptor. No se debe convertir a porcentajes antes de crear el reporte HID.

Pruebas unitarias:

- Estado neutral.
- Cada botón por separado.
- Todos los botones simultáneos.
- Stick mínimo, centro y máximo.
- Trigger mínimo y máximo.
- Valores negativos de sticks.
- Share solamente en Series.

## Fase 5: empaquetado de salida y vibración

Separar completamente la salida de la entrada:

```text
XInputSetState / juego
        |
        v
USB/IP output request
        |
        v
Xbox feedback decoder
        |
        v
FeedbackState
```

El decodificador debe:

- Validar longitud y tipo de reporte.
- Reconocer el formato GIP de salida.
- Reconocer `CFBK` cuando aparezca.
- Ignorar la cabecera `CFBK` al extraer motores.
- Mantener `leftMotor`, `rightMotor`, `leftTrigger` y `rightTrigger` separados.
- No tratar los bytes ASCII `C`, `F`, `B`, `K` como intensidades.
- Enviar estado neutral al terminar.

La vibración no debe cerrar el dispositivo ante un ACK tardío, duplicado o fuera de orden. Un error de feedback debe registrarse y recuperarse sin desmontar la persona USB, siempre que el canal USB/IP siga válido.

## Fase 6: ciclo de vida USB/IP

La persona debe tener un ciclo de vida único:

```text
Create
  -> Advertise
  -> Import
  -> Attached
  -> Active
  -> Neutral
  -> Detach
  -> Destroy
```

Reglas:

- Un solo propietario por persona.
- Un solo bus y puerto por dispositivo.
- No crear un segundo bus durante una reconexión.
- No reutilizar un bus mientras exista una importación activa.
- Esperar confirmación de detach antes de destruir.
- Liberar listeners, goroutines, sockets y archivos de estado.
- Conservar un identificador lógico estable aunque cambie el bus USB/IP.

Esto busca eliminar el problema observado de dispositivos que aparecen y desaparecen después de vibración u órdenes GIP.

## Fase 7: compatibilidad de identidad

La identidad visible debe proceder del perfil:

```text
VID       = perfil
PID       = perfil
Manufacturer = perfil
Product      = perfil
Descriptor   = perfil
```

La identidad interna de VIIPER debe mantenerse separada:

```text
viiperProfile = xbox-one-original | xbox-series-xs
viiperInstance = UUID interno
backend = usbip
```

No se debe colocar `VIIPER` en el producto visible si el objetivo es aproximarse al mando oficial. La trazabilidad se conservará en:

- Metadata de la API.
- Registro del servidor.
- Reporte de pruebas.
- Propiedades internas del estado.

## Fase 8: integración con la API y librería

La API debe aceptar perfiles explícitos:

```json
{
  "type": "xboxone",
  "profile": "xbox-one-original"
}
```

```json
{
  "type": "xboxone",
  "profile": "xbox-series-xs"
}
```

La librería debe exponer constructores equivalentes a los demás mandos, por ejemplo:

```go
NewXboxOneOriginal()
NewXboxSeriesXS()
```

Internamente ambos usarán el mismo encoder, decoder y ciclo de vida.

## Fase 9: compatibilidad XInput

USB/IP puede entregar un dispositivo HID correcto y aun así Windows puede no asignarlo a XInput. Por ello habrá tres niveles de validación:

### Nivel 1: HID

- Device Manager.
- `joy.cpl`.
- HidP/HIDAPI.

### Nivel 2: DirectInput

- Enumeración.
- Ejes.
- Botones.
- Triggers.

### Nivel 3: XInput

- `XInputGetState` devuelve `ERROR_SUCCESS`.
- `XInputGetCapabilities` identifica el dispositivo.
- El juego lo enumera como Xbox.
- La vibración vuelve al servidor.

Si el perfil funciona en HID y DirectInput pero no en XInput, no se considerará una implementación XInput completa. Se documentará como limitación del backend USB/IP y se analizará el enlace con `xinputhid.sys` antes de modificar el resto del sistema.

## Fase 10: pruebas de integración

### Prueba por perfil

```text
crear Xbox One original
esperar 30 segundos
leer identidad
probar todos los botones
mover ambos sticks
probar triggers
activar vibración
detener vibración
desmontar
```

Repetir exactamente para Series X/S.

### Prueba de permanencia

```text
crear
esperar 60 segundos sin entrada
enviar entrada cada 100 ms
enviar vibración cada 500 ms
mantener durante 5 minutos
desmontar limpiamente
```

### Prueba de errores

- Cliente detenido durante vibración.
- ACK retrasado.
- ACK duplicado.
- Reporte de salida incompleto.
- Desconexión de usbip-win2.
- Reinicio del servidor.
- Dos perfiles simultáneos.
- Cuatro mandos del mismo perfil.

### Regresión

Ejecutar todos los mandos existentes:

```text
Xbox 360
DualShock 4
DualSense
Switch 2 Pro
Xbox One original
Xbox Series X/S
```

No debe cambiar la enumeración ni el comportamiento de los cuatro primeros.

## Criterios de aceptación

La reescritura se considerará terminada cuando ambos perfiles cumplan:

- Dispositivo visible en Device Manager.
- Producto, fabricante, VID y PID correctos.
- Descriptor HID válido.
- `joy.cpl` muestra el mando.
- Todos los botones funcionan.
- Sticks y triggers funcionan.
- No hay desconexión durante vibración.
- Vibración izquierda y derecha funcionan o queda documentada la limitación exacta.
- XInput se valida con `XInputGetState` y un juego real.
- El dispositivo sobrevive al menos cinco minutos de uso continuo.
- Cierre y recreación no dejan dispositivos fantasma.
- Las pruebas de regresión de Xbox 360, DS4, DS5 y NS2P pasan.

## Orden de implementación

1. Crear los dos perfiles declarativos.
2. Extraer la lógica común de Xbox One/Series.
3. Implementar descriptores USB/HID.
4. Implementar encoder de entrada.
5. Implementar decoder de salida y vibración.
6. Integrar la persona con el bus USB/IP existente.
7. Corregir el ciclo de vida de attach/detach.
8. Añadir la API y constructores de librería.
9. Probar primero Xbox One original.
10. Probar Xbox Series X/S.
11. Ejecutar pruebas XInput y juego real.
12. Ejecutar pruebas de permanencia y reconexión.
13. Ejecutar regresión de todos los mandos.
14. Cambiar el perfil nuevo a predeterminado solo después de superar la batería completa.

## Decisión técnica

La implementación se hará como una persona USB/IP normal de VIIPER, con dos perfiles Xbox separados y una capa común de reportes. HIDMaestro será una referencia para los descriptores, el orden de reportes, la identidad y las pruebas, pero no será una dependencia de ejecución.

La expectativa realista es:

```text
USB/HID compatible: objetivo directo
DirectInput: objetivo directo
joy.cpl: objetivo directo
XInput: objetivo de validación, dependiente de Windows y xinputhid
Bluetooth auténtico: fuera del alcance de esta reescritura
```
