# Prueba de fan-out de mandos virtuales VIIPER

- **Inicio:** `2026-10-01T22:16:46.485029+00:00`
- **Fin:** `2026-10-01T22:16:50.463867+00:00`
- **Resultado global:** **PASS**
- **VIIPER:** `C:\Users\Consapiens\Documents\Codex\2026-09-22\ana\work\JuanJSAR93-VIIPER\build\viiper.exe`
- **usbip.exe:** `C:\Program Files\USBip\usbip.exe`

Esta prueba crea varias instancias reales del mismo tipo en un bus VIIPER compartido. Cada instancia abre su stream, recibe la matriz de entradas y se verifica mediante `server/status` y `usbip-win2`.

## Resumen por grupo

| Tipo | Cantidad | Bus | Mandos PASS | Resultado |
|---|---:|---:|---:|---|
| `xboxone` | 2 | 1 | 2/2 | **PASS** |

## xboxone x2

Bus VIIPER: `1`

| Instancia | DevId | Producto | VID:PID | USB/IP | Stream | Tests | Resultado |
|---|---|---|---|---|---|---:|---|
| xboxone #1 | `1` | VIIPER Xbox One Controller | `0x045e:0x02d1` | True | True | 35/35 | **PASS** |
| xboxone #2 | `2` | VIIPER Xbox One Controller | `0x045e:0x02d1` | True | True | 35/35 | **PASS** |

### xboxone #1

#### Identidad y estado

| Campo | Valor |
|---|---|
| `busId` | 1 |
| `devId` | 1 |
| `vid` | 0x045e |
| `pid` | 0x02d1 |
| `manufacturer` | ©Microsoft Corporation |
| `product` | VIIPER Xbox One Controller |
| `description` | VIIPER Xbox One Controller |
| `uid` | VIIPER-XBOXONE-0001 |
| `usbipBusId` | 1-1 |
| `usbipImported` | True |
| `inputStreamActive` | True |
| `deviceSpecific` | {"backend": "usbip", "pid": "02D1", "profile": "xboxone", "serial_number": "VIIPER-XBOXONE-0001", "source": "viiper", "vid": "045E", "virtual": true} |

#### Todos los botones y controles

| Control | Fase | Resultado | Detalle |
|---|---|---|---|
| `neutral` | neutral | **PASS** |  |
| `dpad-up` | press | **PASS** |  |
| `dpad-up` | release | **PASS** |  |
| `dpad-down` | press | **PASS** |  |
| `dpad-down` | release | **PASS** |  |
| `dpad-left` | press | **PASS** |  |
| `dpad-left` | release | **PASS** |  |
| `dpad-right` | press | **PASS** |  |
| `dpad-right` | release | **PASS** |  |
| `menu` | press | **PASS** |  |
| `menu` | release | **PASS** |  |
| `view` | press | **PASS** |  |
| `view` | release | **PASS** |  |
| `left-stick` | press | **PASS** |  |
| `left-stick` | release | **PASS** |  |
| `right-stick` | press | **PASS** |  |
| `right-stick` | release | **PASS** |  |
| `left-bumper` | press | **PASS** |  |
| `left-bumper` | release | **PASS** |  |
| `right-bumper` | press | **PASS** |  |
| `right-bumper` | release | **PASS** |  |
| `guide` | press | **PASS** |  |
| `guide` | release | **PASS** |  |
| `A` | press | **PASS** |  |
| `A` | release | **PASS** |  |
| `B` | press | **PASS** |  |
| `B` | release | **PASS** |  |
| `X` | press | **PASS** |  |
| `X` | release | **PASS** |  |
| `Y` | press | **PASS** |  |
| `Y` | release | **PASS** |  |
| `share` | press | **PASS** |  |
| `share` | release | **PASS** |  |
| `triggers-full` | motion | **PASS** |  |
| `sticks-extremes` | motion | **PASS** |  |

#### Salida/estado capturado

```json
{
  "clientOutput": null,
  "createResponse": {
    "busId": 1,
    "devId": "1",
    "deviceSpecific": {
      "backend": "usbip",
      "pid": "02D1",
      "profile": "xboxone",
      "serial_number": "VIIPER-XBOXONE-0001",
      "source": "viiper",
      "vid": "045E",
      "virtual": true
    },
    "pid": "0x02d1",
    "type": "xboxone",
    "vid": "0x045e"
  },
  "statusSnapshot": null
}
```


### xboxone #2

#### Identidad y estado

| Campo | Valor |
|---|---|
| `busId` | 1 |
| `devId` | 2 |
| `vid` | 0x045e |
| `pid` | 0x02d1 |
| `manufacturer` | ©Microsoft Corporation |
| `product` | VIIPER Xbox One Controller |
| `description` | VIIPER Xbox One Controller |
| `uid` | VIIPER-XBOXONE-0001 |
| `usbipBusId` | 1-2 |
| `usbipImported` | True |
| `inputStreamActive` | True |
| `deviceSpecific` | {"backend": "usbip", "pid": "02D1", "profile": "xboxone", "serial_number": "VIIPER-XBOXONE-0001", "source": "viiper", "vid": "045E", "virtual": true} |

#### Todos los botones y controles

| Control | Fase | Resultado | Detalle |
|---|---|---|---|
| `neutral` | neutral | **PASS** |  |
| `dpad-up` | press | **PASS** |  |
| `dpad-up` | release | **PASS** |  |
| `dpad-down` | press | **PASS** |  |
| `dpad-down` | release | **PASS** |  |
| `dpad-left` | press | **PASS** |  |
| `dpad-left` | release | **PASS** |  |
| `dpad-right` | press | **PASS** |  |
| `dpad-right` | release | **PASS** |  |
| `menu` | press | **PASS** |  |
| `menu` | release | **PASS** |  |
| `view` | press | **PASS** |  |
| `view` | release | **PASS** |  |
| `left-stick` | press | **PASS** |  |
| `left-stick` | release | **PASS** |  |
| `right-stick` | press | **PASS** |  |
| `right-stick` | release | **PASS** |  |
| `left-bumper` | press | **PASS** |  |
| `left-bumper` | release | **PASS** |  |
| `right-bumper` | press | **PASS** |  |
| `right-bumper` | release | **PASS** |  |
| `guide` | press | **PASS** |  |
| `guide` | release | **PASS** |  |
| `A` | press | **PASS** |  |
| `A` | release | **PASS** |  |
| `B` | press | **PASS** |  |
| `B` | release | **PASS** |  |
| `X` | press | **PASS** |  |
| `X` | release | **PASS** |  |
| `Y` | press | **PASS** |  |
| `Y` | release | **PASS** |  |
| `share` | press | **PASS** |  |
| `share` | release | **PASS** |  |
| `triggers-full` | motion | **PASS** |  |
| `sticks-extremes` | motion | **PASS** |  |

#### Salida/estado capturado

```json
{
  "clientOutput": null,
  "createResponse": {
    "busId": 1,
    "devId": "2",
    "deviceSpecific": {
      "backend": "usbip",
      "pid": "02D1",
      "profile": "xboxone",
      "serial_number": "VIIPER-XBOXONE-0001",
      "source": "viiper",
      "vid": "045E",
      "virtual": true
    },
    "pid": "0x02d1",
    "type": "xboxone",
    "vid": "0x045e"
  },
  "statusSnapshot": null
}
```
