# application/usecases

`SignIn`, `LoadSession`, `SignOut`, `ListUsers`, `CreateCall`, `LoadCall`, `PrepareDevices`,
`JoinCall`, `SetMicrophoneEnabled`, `SetCameraEnabled`, `StartScreenShare`, `StopScreenShare`,
`LeaveCall`, `EndCall`. Viven en `LabSession`: una sola máquina de estados. Dependen solo de `domain` y `ports`.
