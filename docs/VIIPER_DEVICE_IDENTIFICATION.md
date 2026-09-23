# Identificación de dispositivos VIIPER en Windows

Los mandos VIIPER usan nombres de producto propios para que una aplicación pueda distinguirlos de un mando físico que use el mismo protocolo. En Windows se debe leer `DEVPKEY_Device_BusReportedDeviceDesc` y, cuando sea necesario, corroborar el padre PnP y el servicio del dispositivo.

| Origen | Texto detectado | Evidencia complementaria observada |
| --- | --- | --- |
| VIIPER Xbox 360 | `VIIPER Xbox 360 Controller` | Padre `USB\\ROOT_HUB30...`, servicio `xusb22` |
| VIIPER DS4 | `VIIPER DualShock 4 Controller` | Padre `USB\\ROOT_HUB30...` |
| VIIPER DS5 | `VIIPER DualSense Wireless Controller` | Padre `USB\\ROOT_HUB30...` |
| VIIPER Switch 2 Pro | `VIIPER Switch 2 Pro Controller` | Padre `USB\\ROOT_HUB30...` |
| VIIPER Xbox One | `VIIPER Xbox One Controller` | Clase `XboxComposite`; perfil USB `045E:02EA` y GIP DeviceID/serial único por ejecución |
| VIIPER Xbox Series | `VIIPER Xbox Series X|S Controller` | Perfil USB `045E:0B12` y GIP DeviceID/serial único por ejecución |

El texto del producto es la señal principal. El padre y el servicio son señales de corroboración y pueden cambiar según la versión de Windows, el controlador instalado o la topología USB; no deben usarse como única condición universal.

Ejemplo de consulta en PowerShell:

```powershell
$devices = Get-PnpDevice -PresentOnly | Where-Object {
    $_.Class -in @('XboxComposite', 'HIDClass')
}

foreach ($device in $devices) {
    $props = Get-PnpDeviceProperty -InstanceId $device.InstanceId
    $desc = ($props | Where-Object KeyName -eq 'DEVPKEY_Device_BusReportedDeviceDesc').Data
    if ($desc -like 'VIIPER * Controller') {
        [pscustomobject]@{
            Origin = $desc
            InstanceId = $device.InstanceId
            Class = $device.Class
            Status = $device.Status
        }
    }
}
```
