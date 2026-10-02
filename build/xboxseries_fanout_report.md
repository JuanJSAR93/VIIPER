# Prueba de fan-out de mandos virtuales VIIPER

- **Inicio:** `2026-10-02T14:11:40.814168+00:00`
- **Fin:** `2026-10-02T14:11:42.831658+00:00`
- **Resultado global:** **PASS**
- **VIIPER:** `C:\Users\Consapiens\Documents\Codex\2026-09-22\ana\work\JuanJSAR93-VIIPER\build\viiper.exe`
- **usbip.exe:** `C:\Program Files\USBip\usbip.exe`

Esta prueba crea varias instancias reales del mismo tipo en un bus VIIPER compartido. Cada instancia abre su stream, recibe la matriz de entradas y se verifica mediante `server/status` y `usbip-win2`.

## Resumen por grupo

| Tipo | Cantidad | Bus | Mandos PASS | Resultado |
|---|---:|---:|---:|---|
| `xboxseries` | 1 | 1 | 1/1 | **PASS** |

## xboxseries x1

Bus VIIPER: `1`

| Instancia | DevId | Producto | VID:PID | USB/IP | Stream | Tests | Resultado |
|---|---|---|---|---|---|---:|---|
| xboxseries #1 | `1` | VIIPER Xbox Series X\|S Controller | `0x045e:0x0b12` | True | True | 35/35 | **PASS** |

### xboxseries #1

#### Identidad y estado

| Campo | Valor |
|---|---|
| `busId` | 1 |
| `devId` | 1 |
| `vid` | 0x045e |
| `pid` | 0x0b12 |
| `manufacturer` | ©Microsoft Corporation |
| `product` | VIIPER Xbox Series X\|S Controller |
| `description` | VIIPER Xbox Series X\|S Controller |
| `uid` | VIIPER-XBOXSERIES-0001 |
| `usbipBusId` | 1-1 |
| `usbipImported` | True |
| `inputStreamActive` | True |
| `deviceSpecific` | {"backend": "usbip", "pid": "0B12", "profile": "xboxseries", "serial_number": "VIIPER-XBOXSERIES-0001", "source": "viiper", "vid": "045E", "virtual": true} |

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
      "pid": "0B12",
      "profile": "xboxseries",
      "serial_number": "VIIPER-XBOXSERIES-0001",
      "source": "viiper",
      "vid": "045E",
      "virtual": true
    },
    "pid": "0x0b12",
    "type": "xboxseries",
    "vid": "0x045e"
  },
  "statusSnapshot": null
}
```
