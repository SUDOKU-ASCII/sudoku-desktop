import { Dialogs } from '@wailsio/runtime'

type DestructiveConfirmationOptions = {
  message: string
  cancelLabel: string
  confirmLabel: string
  windows: boolean
}

export const confirmDestructiveAction = async ({
  message,
  cancelLabel,
  confirmLabel,
  windows,
}: DestructiveConfirmationOptions): Promise<boolean> => {
  try {
    // Wails alpha.74 maps Windows question dialogs to the native Yes/No buttons
    // and only invokes callbacks whose labels match those exact result strings.
    const nativeCancelLabel = windows ? 'No' : cancelLabel
    const nativeConfirmLabel = windows ? 'Yes' : confirmLabel
    const action = await Dialogs.Question({
      Message: message,
      Buttons: [
        { Label: nativeCancelLabel, IsCancel: true, IsDefault: true },
        { Label: nativeConfirmLabel },
      ],
    })
    return action === nativeConfirmLabel
  } catch {
    return window.confirm(message)
  }
}
