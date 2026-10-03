// useImages — Cloudinary trade screenshot upload + list.

interface TradeImage {
  id: string
  public_id: string
  secure_url: string
  thumbnail_url: string
  format: string
  width: number
  height: number
  bytes: number
  created_at: string
}

export function useImages() {
  async function listForTrade(tradeId: string): Promise<TradeImage[]> {
    const res = await $fetch<{ images: TradeImage[] }>(`/v1/trades/${tradeId}/images`)
    return res.images
  }

  async function attach(tradeId: string, file: File): Promise<TradeImage> {
    const form = new FormData()
    form.append('image', file)
    const res = await $fetch<{ id: string; public_id: string; secure_url: string; thumbnail_url: string; format: string; width: number; height: number; bytes: number }>(
      `/v1/trades/${tradeId}/images`,
      { method: 'POST', body: form },
    )
    return {
      id: res.id,
      public_id: res.public_id,
      secure_url: res.secure_url,
      thumbnail_url: res.thumbnail_url,
      format: res.format,
      width: res.width,
      height: res.height,
      bytes: res.bytes,
      created_at: new Date().toISOString(),
    }
  }

  return { listForTrade, attach }
}