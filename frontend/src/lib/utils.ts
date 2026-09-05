import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * 标识符（哈希、修订号、长 ID）用中间省略，不用尾部截断：
 * `sha256-066c…5ab5` 还能对照，`sha256-066c9f2a1b` 只是一段无从判断的前缀。
 * 省略号是内容的一部分，缺了它读者不知道自己看到的是被截过的值。
 */
export function truncateIdentifier(value: string, head = 12, tail = 6): string {
  if (value.length <= head + tail + 1) return value
  return `${value.slice(0, head)}…${value.slice(-tail)}`
}
