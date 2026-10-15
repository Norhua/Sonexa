/** 事件触发的串行刷新：同一批/在途事件合并为一次续读，不轮询、不并发堆积。 */
export function createRefreshQueue(work: (current: () => boolean) => Promise<void>) {
  let revision = 0
  let disposed = false
  let running: Promise<void> | null = null

  function request(): Promise<void> {
    if (disposed) return Promise.resolve()
    revision++
    if (!running) {
      running = Promise.resolve().then(async () => {
        try {
          if (disposed) return
          let started: number
          do {
            started = revision
            await work(() => !disposed && started === revision)
          } while (!disposed && started !== revision)
        } finally {
          // 与循环退出处于同一微任务，避免退出与另一个 finally 之间丢掉新事件。
          running = null
        }
      })
    }
    return running
  }

  function dispose() {
    disposed = true
    revision++
  }

  return { request, dispose }
}
