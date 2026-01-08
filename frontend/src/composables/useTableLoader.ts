import { ref, reactive, onUnmounted, toRaw } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import type { BasePaginationResponse, FetchOptions } from '@/types'

interface PaginationState {
  page: number
  page_size: number
  total: number
  pages: number
}

interface TableLoaderOptions<T, P> {
  fetchFn: (page: number, pageSize: number, params: P, options?: FetchOptions) => Promise<BasePaginationResponse<T>>
  initialParams?: P
  pageSize?: number
  debounceMs?: number
}

/**
 * 通用表格数据加载 Composable
 * 统一处理分页、筛选、搜索防抖和请求取消
 */
export function useTableLoader<T, P extends Record<string, any>>(options: TableLoaderOptions<T, P>) {
  const { fetchFn, initialParams, pageSize = 20, debounceMs = 300 } = options

  const items = ref<T[]>([])
  const loading = ref(false)
  const params = reactive<P>({ ...(initialParams || {}) } as P)
  const pagination = reactive<PaginationState>({
    page: 1,
    page_size: pageSize,
    total: 0,
    pages: 0
  })

  let abortController: AbortController | null = null

  const isAbortError = (error: any) => {
    return error?.name === 'AbortError' || error?.code === 'ERR_CANCELED' || error?.name === 'CanceledError'
  }

  const refreshing = ref(false)

  const load = async (silent = false) => {
    if (abortController) {
      abortController.abort()
    }
    abortController = new AbortController()

    if (silent) {
      refreshing.value = true
    } else {
      loading.value = true
    }

    try {
      const response = await fetchFn(
        pagination.page,
        pagination.page_size,
        toRaw(params) as P,
        { signal: abortController.signal }
      )

      items.value = response.items || []
      pagination.total = response.total || 0
      pagination.pages = response.pages || 0
    } catch (error) {
      if (!isAbortError(error)) {
        console.error('Table load error:', error)
        throw error
      }
    } finally {
      // 无论请求成功、失败还是被中止，都需要重置对应的状态
      if (silent) {
        refreshing.value = false
      } else {
        loading.value = false
      }
    }
  }

  // 静默刷新：只更新数据，不显示loading骨架屏，保持滚动位置
  const silentLoad = () => load(true)

  const reload = () => {
    pagination.page = 1
    return load()
  }

  const debouncedReload = useDebounceFn(reload, debounceMs)

  const handlePageChange = (page: number) => {
    pagination.page = page
    load()
  }

  const handlePageSizeChange = (size: number) => {
    pagination.page_size = size
    pagination.page = 1
    load()
  }

  onUnmounted(() => {
    abortController?.abort()
  })

  return {
    items,
    loading,
    refreshing,
    params,
    pagination,
    load,
    silentLoad,
    reload,
    debouncedReload,
    handlePageChange,
    handlePageSizeChange
  }
}
