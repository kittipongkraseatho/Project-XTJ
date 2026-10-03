// useReports — Phase 3 PDF report composable.

export function useReports() {
  // Download the PDF report for the given account+date range.
  // Returns the Blob so the caller can save it or open a preview.
  async function downloadPDF(input: {
    trading_account_id: string
    from: string // YYYY-MM-DD
    to: string // YYYY-MM-DD
    include_equity_curve?: boolean
  }): Promise<Blob> {
    return await $fetch<Blob>('/v1/reports/pdf', {
      method: 'POST',
      body: {
        trading_account_id: input.trading_account_id,
        from: input.from,
        to: input.to,
        include_equity_curve: input.include_equity_curve ?? true,
      },
      // $fetch needs to know not to JSON-parse the response.
      responseType: 'blob',
    })
  }

  // Save a Blob to disk via the browser's download mechanism. Triggered
  // from a user click so popup blockers don't bite.
  function saveBlob(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    // Defer revoke so Safari has time to actually start the download.
    setTimeout(() => URL.revokeObjectURL(url), 1_000)
  }

  return { downloadPDF, saveBlob }
}