/** 只展示后端的精确身份投影，不按名称、曲目绑定或音乐库归属推测。 */
export function candidateReleaseState(row: { releaseExists?: boolean; releaseGroupExists?: boolean }) {
  if (row.releaseExists) {
    return {
      className: 'text-green-700 dark:text-green-400',
      label: 'release 已存在',
      description: '同来源的具体 release 身份已存在于本地，包含同步歌单；不表示该曲目已存在、已入库或可自动导入',
    }
  }
  if (row.releaseGroupExists) {
    return {
      className: 'text-orange-700 dark:text-orange-400',
      label: 'release group 已存在',
      description: '同来源的原生 release group 身份已存在于本地，包含同步歌单；不表示该发行或曲目已存在、已入库或可自动导入',
    }
  }
  return { className: '', label: '', description: '' }
}
