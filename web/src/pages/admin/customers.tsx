import { useQuery, useQueryClient } from "@tanstack/react-query"
import {
  AlertCircle,
  Archive,
  Calendar,
  Check,
  Clock,
  Copy,
  DollarSign,
  Eye,
  FileKey2,
  GitMerge,
  History,
  MoreHorizontal,
  Phone,
  Plus,
  RefreshCw,
  Search,
  ShieldAlert,
  ShieldCheck,
  UserPlus,
} from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  DataTable,
  DataTableBody,
  DataTableCell,
  DataTableEmpty,
  DataTableHead,
  DataTableHeader,
  DataTablePagination,
  DataTableRow,
} from "@/components/ui/data-table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useI18n } from "@/i18n"
import type { CRMCustomerListItem, Plan, Product } from "@/lib/api"
import { admin } from "@/lib/api"
import { formatDate, statusColor } from "@/lib/utils"

export default function CustomersPage() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(0)
  const [search, setSearch] = useState("")
  const [showArchived, setShowArchived] = useState(false)
  const limit = 30

  // Modals state
  const [selectedCustomerId, setSelectedCustomerId] = useState<string | null>(null)
  const [isNewCustomerOpen, setIsNewCustomerOpen] = useState(false)
  const [isSaleOpen, setIsSaleOpen] = useState(false)
  const [isRenewOpen, setIsRenewOpen] = useState<{ customerId: string; licenseId: string } | null>(null)
  const [isMergeOpen, setIsMergeOpen] = useState<string | null>(null)
  const [revealedKey, setRevealedKey] = useState<string | null>(null)
  const [copiedKey, setCopiedKey] = useState(false)

  // Fetch customers list
  const { data, isLoading, refetch } = useQuery({
    queryKey: ["admin", "crm", "customers", search, showArchived, page],
    queryFn: () =>
      admin.crm.listCustomers({
        search: search || undefined,
        archived: showArchived || undefined,
        offset: page * limit,
        limit,
      }),
  })

  // Fetch products & plans for sales
  const { data: productsData } = useQuery({
    queryKey: ["admin", "products", "all"],
    queryFn: () => admin.listProducts({ limit: 100 }),
  })
  const { data: plansData } = useQuery({
    queryKey: ["admin", "plans", "all"],
    queryFn: () => admin.listPlans({ limit: 100 }),
  })

  // Fetch selected customer detail
  const { data: customerDetail, isLoading: isDetailLoading } = useQuery({
    queryKey: ["admin", "crm", "customer", selectedCustomerId],
    queryFn: () => (selectedCustomerId ? admin.crm.getCustomer(selectedCustomerId) : null),
    enabled: Boolean(selectedCustomerId),
  })

  const activeLicenses = customerDetail?.active_licenses ?? []
  const licenseHistory = customerDetail?.license_history ?? []
  const timeline = customerDetail?.timeline ?? []
  const stats = customerDetail?.stats

  const customers = data?.customers || []
  const total = data?.total || 0
  const totalPages = Math.ceil(total / limit)

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedKey(true)
    setTimeout(() => setCopiedKey(false), 2000)
  }

  return (
    <div className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("crm.customers")}</h1>
          <p className="text-muted-foreground">{t("customers.subtitle", { count: total })}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => setIsNewCustomerOpen(true)}>
            <UserPlus className="h-4 w-4 mr-2" />
            {t("crm.newCustomer")}
          </Button>
          <Button onClick={() => setIsSaleOpen(true)} className="bg-primary text-primary-foreground shadow">
            <Plus className="h-4 w-4 mr-2" />
            {t("crm.newSale")}
          </Button>
        </div>
      </div>

      {/* Search and Filters */}
      <div className="flex flex-col sm:flex-row items-center justify-between gap-4">
        <div className="relative flex-1 w-full max-w-md">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Szukaj po nazwisku, telefonie, e-mailu lub kluczu..."
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              setPage(0)
            }}
            className="pl-9"
          />
        </div>
        <div className="flex items-center gap-4 text-sm">
          <label className="flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
              className="rounded border-input text-primary focus:ring-primary h-4 w-4"
            />
            <span className="text-muted-foreground text-xs">{t("crm.filterArchived")}</span>
          </label>
        </div>
      </div>

      {/* Customers Table */}
      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="h-64 animate-pulse bg-muted/40 rounded-lg m-4" />
          ) : (
            <>
              <DataTable>
                <DataTableHeader>
                  <DataTableRow>
                    <DataTableHead>Klient</DataTableHead>
                    <DataTableHead>{t("crm.phone")}</DataTableHead>
                    <DataTableHead>{t("crm.email")}</DataTableHead>
                    <DataTableHead>{t("crm.customerSince")}</DataTableHead>
                    <DataTableHead>Aktualny produkt</DataTableHead>
                    <DataTableHead>Okres</DataTableHead>
                    <DataTableHead>Ważna do</DataTableHead>
                    <DataTableHead>{t("common.status")}</DataTableHead>
                    <DataTableHead className="w-16">{t("common.actions")}</DataTableHead>
                  </DataTableRow>
                </DataTableHeader>
                <DataTableBody>
                  {customers.length === 0 && <DataTableEmpty colSpan={9} message={t("customers.empty")} />}
                  {customers.map((c) => (
                    <DataTableRow
                      key={c.id}
                      className="cursor-pointer hover:bg-muted/50"
                      onClick={() => setSelectedCustomerId(c.id)}
                    >
                      <DataTableCell>
                        <div className="font-semibold text-foreground">
                          {c.first_name} {c.last_name}
                        </div>
                        {c.archived_at && (
                          <span className="inline-flex items-center gap-1 text-xs text-amber-500 mt-0.5">
                            <Archive className="h-3 w-3" /> Zarchiwizowany
                          </span>
                        )}
                      </DataTableCell>
                      <DataTableCell className="font-mono text-xs font-medium text-muted-foreground">
                        {c.phone}
                      </DataTableCell>
                      <DataTableCell className="text-xs text-muted-foreground">{c.email || "—"}</DataTableCell>
                      <DataTableCell className="text-xs text-muted-foreground">
                        {formatDate(c.customer_since)}
                      </DataTableCell>
                      <DataTableCell className="text-xs font-medium">
                        {c.current_product ? (
                          <div className="flex items-center gap-1.5">
                            <ShieldCheck className="h-3.5 w-3.5 text-primary" />
                            <span>{c.current_product}</span>
                          </div>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </DataTableCell>
                      <DataTableCell className="text-xs text-muted-foreground">
                        {c.current_plan ? `${c.current_plan}` : "—"}
                      </DataTableCell>
                      <DataTableCell className="text-xs text-muted-foreground font-mono">
                        {c.valid_until ? formatDate(c.valid_until) : c.current_product ? "Wieczysta" : "—"}
                      </DataTableCell>
                      <DataTableCell>
                        <Badge variant={statusColor(c.status) as any} className="capitalize text-xs">
                          {c.status === "no_license" ? "Brak licencji" : c.status}
                        </Badge>
                      </DataTableCell>
                      <DataTableCell onClick={(e) => e.stopPropagation()}>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-foreground"
                          onClick={() => setSelectedCustomerId(c.id)}
                        >
                          <Eye className="h-4 w-4" />
                        </Button>
                      </DataTableCell>
                    </DataTableRow>
                  ))}
                </DataTableBody>
              </DataTable>
              {total > 0 && (
                <div className="p-4 border-t">
                  <DataTablePagination
                    page={page}
                    totalPages={totalPages}
                    total={total}
                    pageSize={limit}
                    onPageChange={setPage}
                  />
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {/* ─── Customer Profile Modal ─── */}
      {selectedCustomerId && (
        <Dialog open={Boolean(selectedCustomerId)} onOpenChange={() => setSelectedCustomerId(null)}>
          <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
            {isDetailLoading || !customerDetail || !customerDetail.customer ? (
              <div className="py-16 text-center text-muted-foreground">Ładowanie kartoteki klienta...</div>
            ) : (
              <div className="space-y-6">
                {/* Header */}
                <DialogHeader className="border-b pb-4">
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                    <div>
                      <DialogTitle className="text-2xl font-bold tracking-tight">
                        {customerDetail.customer.first_name} {customerDetail.customer.last_name}
                      </DialogTitle>
                      <DialogDescription className="flex flex-wrap items-center gap-4 mt-2 text-sm">
                        <span className="flex items-center gap-1.5 font-mono text-foreground font-medium">
                          <Phone className="h-3.5 w-3.5 text-primary" />
                          {customerDetail.customer.phone}
                        </span>
                        {customerDetail.customer.email && (
                          <span className="flex items-center gap-1.5 text-muted-foreground">
                            {customerDetail.customer.email}
                          </span>
                        )}
                        <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                          <Calendar className="h-3.5 w-3.5" />
                          {t("crm.customerSince")}: {formatDate(customerDetail.customer.customer_since)}
                        </span>
                      </DialogDescription>
                    </div>

                    <div className="flex items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setIsSaleOpen(true)
                        }}
                      >
                        <Plus className="h-4 w-4 mr-1.5" />
                        Nowa licencja
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-8 w-8">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => setIsMergeOpen(customerDetail.customer.id)}>
                            <GitMerge className="h-4 w-4 mr-2" />
                            {t("crm.merge")}
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="text-destructive"
                            onClick={() => {
                              if (confirm(t("crm.archiveConfirm"))) {
                                admin.crm.archiveCustomer(customerDetail.customer.id).then(() => {
                                  refetch()
                                  setSelectedCustomerId(null)
                                })
                              }
                            }}
                          >
                            <Archive className="h-4 w-4 mr-2" />
                            {t("crm.archive")}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </div>

                  {customerDetail.customer.notes && (
                    <div className="mt-3 p-2.5 rounded bg-muted/50 border text-xs text-muted-foreground">
                      <span className="font-semibold text-foreground mr-1.5">Notatki:</span>
                      {customerDetail.customer.notes}
                    </div>
                  )}
                </DialogHeader>

                {/* KPI Statistics Cards */}
                <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
                  <Card className="bg-muted/30">
                    <CardContent className="p-3">
                      <div className="text-xs text-muted-foreground font-medium flex items-center gap-1">
                        <ShieldCheck className="h-3.5 w-3.5 text-primary" />
                        {t("crm.activeLicenses")}
                      </div>
                      <div className="text-xl font-bold mt-1">{stats?.active_licenses_count ?? 0}</div>
                    </CardContent>
                  </Card>

                  <Card className="bg-muted/30">
                    <CardContent className="p-3">
                      <div className="text-xs text-muted-foreground font-medium flex items-center gap-1">
                        <Clock className="h-3.5 w-3.5 text-blue-500" />
                        {t("crm.activeTime")}
                      </div>
                      <div className="text-xl font-bold mt-1">{stats?.active_days ?? 0} dni</div>
                    </CardContent>
                  </Card>

                  <Card className="bg-muted/30">
                    <CardContent className="p-3">
                      <div className="text-xs text-muted-foreground font-medium flex items-center gap-1">
                        <DollarSign className="h-3.5 w-3.5 text-emerald-500" />
                        {t("crm.totalPurchases")}
                      </div>
                      <div className="text-xl font-bold mt-1">{stats?.purchases_count ?? 0}</div>
                    </CardContent>
                  </Card>

                  <Card className="bg-muted/30">
                    <CardContent className="p-3">
                      <div className="text-xs text-muted-foreground font-medium flex items-center gap-1">
                        <RefreshCw className="h-3.5 w-3.5 text-purple-500" />
                        {t("crm.renewalsCount")}
                      </div>
                      <div className="text-xl font-bold mt-1">{stats?.renewals_count ?? 0}</div>
                    </CardContent>
                  </Card>

                  <Card className="bg-muted/30">
                    <CardContent className="p-3">
                      <div className="text-xs text-muted-foreground font-medium flex items-center gap-1">
                        <ShieldAlert className="h-3.5 w-3.5 text-amber-500" />
                        {t("crm.protectionGaps")}
                      </div>
                      <div className="text-xl font-bold mt-1 text-amber-600 dark:text-amber-400">
                        {stats?.gaps_count ?? 0} ({stats?.gap_days ?? 0} dni)
                      </div>
                    </CardContent>
                  </Card>

                  <Card className="bg-muted/30">
                    <CardContent className="p-3">
                      <div className="text-xs text-muted-foreground font-medium flex items-center gap-1">
                        <Calendar className="h-3.5 w-3.5 text-muted-foreground" />
                        {t("crm.lastPurchase")}
                      </div>
                      <div className="text-sm font-semibold mt-1.5 truncate">
                        {stats?.last_purchase_date ? formatDate(stats.last_purchase_date) : "—"}
                      </div>
                    </CardContent>
                  </Card>
                </div>

                {/* Tabs Sections */}
                <Tabs defaultValue="active_licenses" className="w-full">
                  <TabsList className="grid w-full grid-cols-3">
                    <TabsTrigger value="active_licenses">
                      {t("crm.currentLicenses")} ({activeLicenses.length})
                    </TabsTrigger>
                    <TabsTrigger value="history_licenses">
                      {t("crm.licenseHistory")} ({licenseHistory.length})
                    </TabsTrigger>
                    <TabsTrigger value="timeline">
                      {t("crm.timeline")} ({timeline.length})
                    </TabsTrigger>
                  </TabsList>

                  {/* Tab 1: Current Licenses */}
                  <TabsContent value="active_licenses" className="mt-4 space-y-4">
                    {activeLicenses.length === 0 ? (
                      <div className="p-8 text-center border rounded-lg bg-muted/20 text-muted-foreground">
                        Brak aktywnych licencji dla tego klienta.
                      </div>
                    ) : (
                      activeLicenses.map((lic) => (
                        <Card key={lic.id} className="border-l-4 border-l-primary">
                          <CardHeader className="p-4 pb-2">
                            <div className="flex items-center justify-between">
                              <div className="space-y-0.5">
                                <CardTitle className="text-base font-bold flex items-center gap-2">
                                  {lic.product?.name || "Multi-Guard"} — {lic.plan?.name}
                                  <Badge variant={statusColor(lic.status) as any} className="capitalize text-xs">
                                    {lic.status}
                                  </Badge>
                                </CardTitle>
                                <CardDescription className="text-xs">
                                  Model: {lic.plan?.license_type || "subskrypcja"} (
                                  {lic.plan?.billing_interval || "roczna"})
                                </CardDescription>
                              </div>

                              <div className="flex items-center gap-2">
                                <Button
                                  variant="outline"
                                  size="sm"
                                  onClick={() =>
                                    setIsRenewOpen({
                                      customerId: customerDetail.customer.id,
                                      licenseId: lic.id,
                                    })
                                  }
                                >
                                  <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
                                  {t("crm.renew")}
                                </Button>
                              </div>
                            </div>
                          </CardHeader>
                          <CardContent className="p-4 pt-2 text-xs space-y-3">
                            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 pt-2 border-t text-muted-foreground">
                              <div>
                                <span className="block font-medium text-foreground">Ważna od:</span>
                                {formatDate(lic.created_at)}
                              </div>
                              <div>
                                <span className="block font-medium text-foreground">Ważna do:</span>
                                {lic.valid_until ? formatDate(lic.valid_until) : "Bezterminowa"}
                              </div>
                              <div>
                                <span className="block font-medium text-foreground">Aktywne urządzenia:</span>
                                {lic.activations_count} z {lic.plan?.max_activations || "—"}
                              </div>
                              <div>
                                <span className="block font-medium text-foreground">Klucz:</span>
                                <div className="flex items-center gap-1.5 font-mono font-bold text-foreground">
                                  <span>{lic.license_key_hint || "••••-••••"}</span>
                                  <button
                                    type="button"
                                    className="text-primary hover:underline font-normal text-[11px]"
                                    onClick={() => {
                                      admin.revealLicenseKey(lic.id).then((res) => {
                                        setRevealedKey(res.license_key)
                                      })
                                    }}
                                  >
                                    [Pokaż]
                                  </button>
                                </div>
                              </div>
                            </div>
                          </CardContent>
                        </Card>
                      ))
                    )}
                  </TabsContent>

                  {/* Tab 2: License History */}
                  <TabsContent value="history_licenses" className="mt-4">
                    {licenseHistory.length === 0 ? (
                      <div className="p-8 text-center border rounded-lg bg-muted/20 text-muted-foreground">
                        Brak historii licencji dla tego klienta.
                      </div>
                    ) : (
                      <Card>
                        <CardContent className="p-0">
                          <DataTable>
                            <DataTableHeader>
                              <DataTableRow>
                                <DataTableHead>Produkt i Plan</DataTableHead>
                                <DataTableHead>Okres licencji</DataTableHead>
                                <DataTableHead>Klucz</DataTableHead>
                                <DataTableHead>Status</DataTableHead>
                                <DataTableHead>Data przypisania</DataTableHead>
                              </DataTableRow>
                            </DataTableHeader>
                            <DataTableBody>
                              {licenseHistory.map((lic) => (
                                <DataTableRow key={lic.id}>
                                  <DataTableCell className="text-xs font-semibold">
                                    {lic.product?.name || "Multi-Guard"} ({lic.plan?.name || "Standard"})
                                  </DataTableCell>
                                  <DataTableCell className="text-xs text-muted-foreground font-mono">
                                    {formatDate(lic.created_at)} →{" "}
                                    {lic.valid_until ? formatDate(lic.valid_until) : "Wieczysta"}
                                  </DataTableCell>
                                  <DataTableCell className="text-xs font-mono">
                                    {lic.license_key_hint || "••••-••••"}
                                  </DataTableCell>
                                  <DataTableCell>
                                    <Badge variant={statusColor(lic.status) as any} className="capitalize text-xs">
                                      {lic.status}
                                    </Badge>
                                  </DataTableCell>
                                  <DataTableCell className="text-xs text-muted-foreground">
                                    {formatDate(lic.assigned_at)}
                                  </DataTableCell>
                                </DataTableRow>
                              ))}
                            </DataTableBody>
                          </DataTable>
                        </CardContent>
                      </Card>
                    )}
                  </TabsContent>

                  {/* Tab 3: History & Timeline */}
                  <TabsContent value="timeline" className="mt-4">
                    {timeline.length === 0 ? (
                      <div className="p-8 text-center border rounded-lg bg-muted/20 text-muted-foreground">
                        Brak zdarzeń w osi czasu klienta.
                      </div>
                    ) : (
                      <div className="space-y-3">
                        {timeline.map((item) => (
                          <div
                            key={item.id}
                            className={`p-3.5 rounded-lg border text-sm transition-all ${
                              item.kind === "gap"
                                ? "bg-amber-500/10 border-amber-500/30 text-amber-950 dark:text-amber-200"
                                : "bg-card hover:bg-muted/40"
                            }`}
                          >
                            <div className="flex items-start justify-between gap-4">
                              <div className="space-y-1">
                                <div className="flex items-center gap-2">
                                  {item.kind === "gap" ? (
                                    <AlertCircle className="h-4 w-4 text-amber-500 shrink-0" />
                                  ) : (
                                    <History className="h-4 w-4 text-primary shrink-0" />
                                  )}
                                  <span className="font-semibold">{item.title}</span>
                                  {item.amount != null && (
                                    <Badge variant="outline" className="text-xs font-mono font-semibold">
                                      {Number(item.amount).toFixed(2)} {item.currency || "PLN"}
                                    </Badge>
                                  )}
                                  {item.payment_method && (
                                    <Badge variant="secondary" className="text-[11px] capitalize">
                                      {item.payment_method}
                                    </Badge>
                                  )}
                                </div>
                                {item.description && (
                                  <p className="text-xs text-muted-foreground pl-6">{item.description}</p>
                                )}
                                {item.period_from && item.period_until && (
                                  <p className="text-[11px] text-muted-foreground pl-6 font-mono">
                                    Okres: {formatDate(item.period_from)} → {formatDate(item.period_until)}
                                  </p>
                                )}
                              </div>
                              <div className="text-xs text-muted-foreground shrink-0 font-mono">
                                {formatDate(item.date)}
                              </div>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </TabsContent>
                </Tabs>
              </div>
            )}
          </DialogContent>
        </Dialog>
      )}

      {/* ─── Modal: Nowa Sprzedaż ─── */}
      {isSaleOpen && (
        <SaleDialog
          open={isSaleOpen}
          preselectedCustomerId={selectedCustomerId}
          onClose={() => setIsSaleOpen(false)}
          onSuccess={(newLicKey) => {
            setIsSaleOpen(false)
            setRevealedKey(newLicKey)
            refetch()
            if (selectedCustomerId) {
              queryClient.invalidateQueries({ queryKey: ["admin", "crm", "customer", selectedCustomerId] })
            }
          }}
          products={productsData?.products || []}
          plans={plansData?.plans || []}
        />
      )}

      {/* ─── Modal: Nowy Klient ─── */}
      {isNewCustomerOpen && (
        <NewCustomerDialog
          open={isNewCustomerOpen}
          onClose={() => setIsNewCustomerOpen(false)}
          onSuccess={() => {
            setIsNewCustomerOpen(false)
            refetch()
          }}
        />
      )}

      {/* ─── Modal: Przedłuż Licencję ─── */}
      {isRenewOpen && (
        <RenewDialog
          open={Boolean(isRenewOpen)}
          customerId={isRenewOpen.customerId}
          licenseId={isRenewOpen.licenseId}
          onClose={() => setIsRenewOpen(null)}
          onSuccess={() => {
            setIsRenewOpen(null)
            refetch()
            queryClient.invalidateQueries({ queryKey: ["admin", "crm", "customer", isRenewOpen.customerId] })
          }}
        />
      )}

      {/* ─── Modal: Scal Klienta (Merge) ─── */}
      {isMergeOpen && (
        <MergeCustomerDialog
          open={Boolean(isMergeOpen)}
          sourceCustomerId={isMergeOpen}
          customers={customers.filter((c) => c.id !== isMergeOpen)}
          onClose={() => setIsMergeOpen(null)}
          onSuccess={() => {
            setIsMergeOpen(null)
            setSelectedCustomerId(null)
            refetch()
          }}
        />
      )}

      {/* ─── Modal: Prezentacja Klucza Licencyjnego ─── */}
      {revealedKey && (
        <Dialog open={Boolean(revealedKey)} onOpenChange={() => setRevealedKey(null)}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <FileKey2 className="h-5 w-5 text-primary" />
                Klucz licencyjny klienta
              </DialogTitle>
              <DialogDescription>
                Poniższy klucz licencyjny został wygenerowany i zabezpieczony. Przekaż go klientowi:
              </DialogDescription>
            </DialogHeader>
            <div className="p-4 bg-muted rounded-lg font-mono text-center text-lg font-bold select-all tracking-wider break-all border">
              {revealedKey}
            </div>
            <DialogFooter className="gap-2">
              <Button variant="outline" className="w-full sm:w-auto" onClick={() => copyToClipboard(revealedKey)}>
                {copiedKey ? <Check className="h-4 w-4 mr-2" /> : <Copy className="h-4 w-4 mr-2" />}
                {copiedKey ? t("common.copied") : t("crm.copyKey")}
              </Button>
              <Button onClick={() => setRevealedKey(null)}>{t("common.close")}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

// ─── Sub-Dialogs ───

function NewCustomerDialog({
  open,
  onClose,
  onSuccess,
}: {
  open: boolean
  onClose: () => void
  onSuccess: () => void
}) {
  const [firstName, setFirstName] = useState("")
  const [lastName, setLastName] = useState("")
  const [phone, setPhone] = useState("")
  const [email, setEmail] = useState("")
  const [notes, setNotes] = useState("")
  const [duplicateWarning, setDuplicateWarning] = useState<string | null>(null)

  const checkDup = async (ph: string, em: string) => {
    if (!ph && !em) return
    try {
      const res = await admin.crm.checkDuplicates({ phone: ph, email: em })
      if (res.has_duplicates) {
        setDuplicateWarning("Uwaga: W bazie istnieje już klient o tym samym numerze telefonu lub e-mailu!")
      } else {
        setDuplicateWarning(null)
      }
    } catch {
      // ignore
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!firstName || !lastName || !phone) return
    await admin.crm.createCustomer({
      first_name: firstName,
      last_name: lastName,
      phone,
      email: email || undefined,
      notes,
    })
    onSuccess()
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Nowy Klient Multi-Servis</DialogTitle>
            <DialogDescription>Wprowadź dane klienta do bazy CRM.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Imię *</Label>
                <Input value={firstName} onChange={(e) => setFirstName(e.target.value)} required />
              </div>
              <div className="space-y-2">
                <Label>Nazwisko *</Label>
                <Input value={lastName} onChange={(e) => setLastName(e.target.value)} required />
              </div>
            </div>

            <div className="space-y-2">
              <Label>Telefon (komórkowy/stacjonarny) *</Label>
              <Input
                placeholder="np. 500 123 456 lub +48500123456"
                value={phone}
                onChange={(e) => {
                  setPhone(e.target.value)
                  checkDup(e.target.value, email)
                }}
                required
              />
            </div>

            <div className="space-y-2">
              <Label>E-mail (opcjonalny)</Label>
              <Input
                type="email"
                placeholder="klient@example.com"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value)
                  checkDup(phone, e.target.value)
                }}
              />
            </div>

            {duplicateWarning && (
              <div className="p-3 bg-amber-500/15 border border-amber-500/30 rounded text-xs text-amber-700 dark:text-amber-300 flex items-center gap-2">
                <AlertCircle className="h-4 w-4 shrink-0" />
                <span>{duplicateWarning}</span>
              </div>
            )}

            <div className="space-y-2">
              <Label>Notatki serwisowe</Label>
              <Input placeholder="Dodatkowe informacje..." value={notes} onChange={(e) => setNotes(e.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Anuluj
            </Button>
            <Button type="submit">Utwórz klienta</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function SaleDialog({
  open,
  preselectedCustomerId,
  onClose,
  onSuccess,
  products,
  plans,
}: {
  open: boolean
  preselectedCustomerId: string | null
  onClose: () => void
  onSuccess: (licenseKey: string) => void
  products: Product[]
  plans: Plan[]
}) {
  const [customerId, setCustomerId] = useState(preselectedCustomerId || "")
  const [productId, setProductId] = useState("")
  const [planId, setPlanId] = useState("")
  const [paymentMethod, setPaymentMethod] = useState("cash")
  const [amount, setAmount] = useState("149.00")
  const [currency, setCurrency] = useState("PLN")
  const [customerSearch, setCustomerSearch] = useState("")
  const [selectedCustName, setSelectedCustName] = useState("")
  const [isSubmitting, setIsSubmitting] = useState(false)

  // Customer search for sale
  const { data: searchResults } = useQuery({
    queryKey: ["admin", "crm", "customer_search", customerSearch],
    queryFn: () => admin.crm.listCustomers({ search: customerSearch, limit: 5 }),
    enabled: customerSearch.length >= 2,
  })

  const availablePlans = plans.filter((p) => p.product_id === productId)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!customerId || !productId || !planId) return
    setIsSubmitting(true)
    try {
      const res = await admin.crm.createSale(customerId, {
        product_id: productId,
        plan_id: planId,
        payment_method: paymentMethod,
        amount: Number.parseFloat(amount) || 0,
        currency,
      })
      onSuccess(res.license_key)
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Nowa Sprzedaż Licencji</DialogTitle>
            <DialogDescription>Wystawienie licencji przez KeyGate z przypisaniem do klienta CRM.</DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            {/* Customer selector */}
            {!preselectedCustomerId ? (
              <div className="space-y-2">
                <Label>Wybierz klienta</Label>
                {selectedCustName ? (
                  <div className="flex items-center justify-between p-2.5 rounded border bg-muted/40">
                    <span className="text-sm font-semibold">{selectedCustName}</span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setCustomerId("")
                        setSelectedCustName("")
                      }}
                    >
                      Zmień
                    </Button>
                  </div>
                ) : (
                  <div className="space-y-1 relative">
                    <Input
                      placeholder="Wpisz nazwisko lub telefon klienta..."
                      value={customerSearch}
                      onChange={(e) => setCustomerSearch(e.target.value)}
                    />
                    {(searchResults?.customers?.length ?? 0) > 0 && (
                      <div className="absolute z-10 w-full mt-1 bg-popover border rounded-md shadow-lg overflow-hidden">
                        {(searchResults?.customers ?? []).map((c) => (
                          <button
                            type="button"
                            key={c.id}
                            className="w-full text-left p-2.5 hover:bg-accent cursor-pointer text-sm flex justify-between items-center transition-colors"
                            onClick={() => {
                              setCustomerId(c.id)
                              setSelectedCustName(`${c.first_name} ${c.last_name} (${c.phone})`)
                              setCustomerSearch("")
                            }}
                          >
                            <span className="font-medium">
                              {c.first_name} {c.last_name}
                            </span>
                            <span className="text-xs text-muted-foreground font-mono">{c.phone}</span>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ) : null}

            {/* Product and Plan */}
            <div className="space-y-2">
              <Label>Produkt</Label>
              <Select
                value={productId}
                onValueChange={(val) => {
                  setProductId(val)
                  setPlanId("")
                }}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Wybierz produkt..." />
                </SelectTrigger>
                <SelectContent>
                  {products.map((prod) => (
                    <SelectItem key={prod.id} value={prod.id}>
                      {prod.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {productId && (
              <div className="space-y-2">
                <Label>Plan</Label>
                <Select value={planId} onValueChange={setPlanId}>
                  <SelectTrigger>
                    <SelectValue placeholder="Wybierz plan..." />
                  </SelectTrigger>
                  <SelectContent>
                    {availablePlans.map((plan) => (
                      <SelectItem key={plan.id} value={plan.id}>
                        {plan.name} ({plan.billing_interval || plan.license_type})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {/* Payment info */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Sposób płatności</Label>
                <Select value={paymentMethod} onValueChange={setPaymentMethod}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="cash">Gotówka</SelectItem>
                    <SelectItem value="bank_transfer">Przelew bankowy</SelectItem>
                    <SelectItem value="card_manual">Karta płatnicza</SelectItem>
                    <SelectItem value="stripe">Stripe</SelectItem>
                    <SelectItem value="paypal">PayPal</SelectItem>
                    <SelectItem value="other">Inna</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label>Kwota</Label>
                <div className="flex gap-2">
                  <Input
                    type="number"
                    step="0.01"
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                    required
                  />
                  <Input className="w-20" value={currency} onChange={(e) => setCurrency(e.target.value)} />
                </div>
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Anuluj
            </Button>
            <Button type="submit" disabled={!customerId || !productId || !planId || isSubmitting}>
              {isSubmitting ? "Wystawianie..." : "Wystaw licencję i zarejestruj"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RenewDialog({
  open,
  customerId,
  licenseId,
  onClose,
  onSuccess,
}: {
  open: boolean
  customerId: string
  licenseId: string
  onClose: () => void
  onSuccess: () => void
}) {
  const [durationMonths, setDurationMonths] = useState("12")
  const [paymentMethod, setPaymentMethod] = useState("cash")
  const [amount, setAmount] = useState("149.00")
  const [currency, setCurrency] = useState("PLN")
  const [notes, setNotes] = useState("")

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    await admin.crm.renewLicense(customerId, licenseId, {
      duration_months: Number.parseInt(durationMonths, 10) || 12,
      payment_method: paymentMethod,
      amount: Number.parseFloat(amount) || 0,
      currency,
      notes,
    })
    onSuccess()
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Przedłużenie licencji</DialogTitle>
            <DialogDescription>Zarejestruj odnowienie licencji klienta.</DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Okres przedłużenia</Label>
              <Select value={durationMonths} onValueChange={setDurationMonths}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">1 miesiąc</SelectItem>
                  <SelectItem value="12">1 rok (12 miesięcy)</SelectItem>
                  <SelectItem value="24">2 lata (24 miesiące)</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Sposób płatności</Label>
                <Select value={paymentMethod} onValueChange={setPaymentMethod}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="cash">Gotówka</SelectItem>
                    <SelectItem value="bank_transfer">Przelew</SelectItem>
                    <SelectItem value="card_manual">Karta</SelectItem>
                    <SelectItem value="stripe">Stripe</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label>Kwota</Label>
                <div className="flex gap-2">
                  <Input type="number" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} />
                  <Input className="w-20" value={currency} onChange={(e) => setCurrency(e.target.value)} />
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <Label>Notatki</Label>
              <Input
                placeholder="np. przedłużenie stacjonarne w serwisie"
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
              />
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Anuluj
            </Button>
            <Button type="submit">Przedłuż licencję</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function MergeCustomerDialog({
  open,
  sourceCustomerId,
  customers,
  onClose,
  onSuccess,
}: {
  open: boolean
  sourceCustomerId: string
  customers: CRMCustomerListItem[]
  onClose: () => void
  onSuccess: () => void
}) {
  const [targetId, setTargetId] = useState("")

  const handleMerge = async () => {
    if (!targetId) return
    if (confirm("Czy na pewno chcesz scalić tego klienta? Wszystkie licencje i historia zostaną przeniesione.")) {
      await admin.crm.mergeCustomers({
        source_customer_id: sourceCustomerId,
        target_customer_id: targetId,
      })
      onSuccess()
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Scalanie klientów (Merge Duplicates)</DialogTitle>
          <DialogDescription>
            Wszystkie licencje, płatności i zdarzenia zostaną bezpiecznie przeniesione do wybranego profilu głównego.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <Label>Wybierz docelowego klienta głównego:</Label>
          <Select value={targetId} onValueChange={setTargetId}>
            <SelectTrigger>
              <SelectValue placeholder="Wybierz klienta docelowego..." />
            </SelectTrigger>
            <SelectContent>
              {(customers ?? []).map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.first_name} {c.last_name} ({c.phone})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Anuluj
          </Button>
          <Button disabled={!targetId} onClick={handleMerge}>
            Scal klientów
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
