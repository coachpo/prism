import * as React from "react"

/** 浮层自己的内容；焦点进到这里面不算「打开它的那个元素」。 */
const OVERLAY_CONTENT_SELECTOR =
  "[data-slot=dialog-content],[data-slot=sheet-content],[data-slot=alert-dialog-content]"

/**
 * 菜单项、下拉选项这类元素在浮层打开时就已经卸载了，把焦点还给它们没有意义；
 * 该还回去的是打开菜单的那个触发器。
 */
const TRANSIENT_CONTAINER_SELECTOR =
  "[role=menu],[role=listbox],[data-slot=dropdown-menu-content],[data-slot=select-content]"

let lastStableFocus: HTMLElement | null = null

function isStableOpener(element: Element | null): element is HTMLElement {
  return (
    element instanceof HTMLElement &&
    element !== document.body &&
    !element.closest(OVERLAY_CONTENT_SELECTOR) &&
    !element.closest(TRANSIENT_CONTAINER_SELECTOR)
  )
}

/**
 * 焦点落进菜单或下拉时，该记住的是打开它的触发器 —— Radix 用
 * `aria-controls` 把两者关联起来。从菜单项打开对话框时，菜单会直接把焦点
 * 交给对话框，触发器一次都不会重新获得焦点，所以只能在这里回溯。
 */
function resolveTransientOwner(container: Element): HTMLElement | null {
  const owner = container.id
    ? document.querySelector(`[aria-controls="${CSS.escape(container.id)}"]`)
    : null
  return owner instanceof HTMLElement ? owner : null
}

if (typeof document !== "undefined") {
  document.addEventListener(
    "focusin",
    (event) => {
      const target = event.target
      if (!(target instanceof HTMLElement)) return
      if (target.closest(OVERLAY_CONTENT_SELECTOR)) return

      const transientContainer = target.closest(TRANSIENT_CONTAINER_SELECTOR)
      if (transientContainer) {
        const owner = resolveTransientOwner(transientContainer)
        if (owner) lastStableFocus = owner
        return
      }

      if (target !== document.body) lastStableFocus = target
    },
    true,
  )
}

/**
 * 受控的 Dialog/Sheet/AlertDialog 没有 Radix Trigger，Radix 无从得知焦点来源，
 * 关闭后会把焦点丢回 body —— 键盘用户每取消一次就要重新 Tab 几十次回到原处。
 *
 * 从行菜单打开时时序更绕：浮层首次渲染的那一刻焦点还在正在关闭的菜单上，
 * 触发器要到下一帧才拿回焦点。所以这里先在渲染期抓一个候选，浮层挂载后再补
 * 一次，取全局记录的最近一个稳定焦点。
 *
 * 必须在只有浮层打开时才挂载的组件里调用（Portal 内部），否则记录到的是
 * 页面加载时的焦点。
 */
export function useCloseFocusRestore(onCloseAutoFocus?: (event: Event) => void) {
  const openerRef = React.useRef<HTMLElement | null>(null)
  const [initialOpener] = React.useState<HTMLElement | null>(() => {
    if (typeof document === "undefined") return null
    const active = document.activeElement
    return isStableOpener(active) ? active : lastStableFocus
  })
  if (openerRef.current === null) openerRef.current = initialOpener

  // 浮层打开期间页面其它元素拿不到焦点，浮层内部的焦点又被过滤掉，
  // 所以到关闭这一刻，全局记录的最近稳定焦点就是打开它的那个元素。
  const resolveOpener = () =>
    openerRef.current?.isConnected ? openerRef.current : lastStableFocus

  React.useEffect(
    () => () => {
      const opener = resolveOpener()
      if (!opener?.isConnected) return
      // 调用方常用 `{state ? <Dialog open .../> : null}` 关闭浮层，这会直接卸载
      // 组件，Radix 的 onCloseAutoFocus 根本没机会跑。这是同一件事的兜底。
      requestAnimationFrame(() => {
        if (!opener.isConnected) return
        const active = document.activeElement
        if (active === null || active === document.body) opener.focus()
      })
    },
    [],
  )

  return React.useCallback(
    (event: Event) => {
      onCloseAutoFocus?.(event)
      if (event.defaultPrevented) return
      const opener = resolveOpener()
      if (!opener?.isConnected) return
      event.preventDefault()
      opener.focus()
    },
    [onCloseAutoFocus],
  )
}
