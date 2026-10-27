// Default category tree copied to every new user. Keys are stable: built-in merchant rules and insights target them.
// Transactions without a category are "Non catégorisé" (category_id NULL), so that one is not a row.
package categories

type seed struct {
	key, name, kind, icon, color string
	excluded                     bool
	children                     []seed
}

func expense(key, name, icon, color string, children ...seed) seed {
	return seed{key: key, name: name, kind: "expense", icon: icon, color: color, children: children}
}

// Colors are the shared categorical palette (moneta-app packages/shared categories.ts uses the same values).
var defaults = []seed{
	expense("food", "Alimentation", "shopping-cart", "#2E7D32",
		expense("groceries", "Courses", "shopping-basket", "#2E7D32"),
		expense("restaurants", "Restaurants", "utensils", "#558B2F")),
	expense("housing", "Logement", "home", "#1565C0",
		expense("rent", "Loyer", "key", "#1565C0"),
		expense("energy", "Énergie", "zap", "#0277BD"),
		expense("telecom", "Internet & téléphone", "wifi", "#00838F"),
		expense("home_insurance", "Assurance habitation", "shield", "#283593")),
	expense("transport", "Transport", "car", "#6A1B9A",
		expense("fuel", "Carburant", "fuel", "#6A1B9A"),
		expense("public_transport", "Transports en commun", "bus", "#8E24AA"),
		expense("travel", "Train & avion", "train", "#AB47BC"),
		expense("parking", "Péage & parking", "parking", "#7B1FA2")),
	expense("subscriptions", "Abonnements", "repeat", "#C62828",
		expense("streaming", "Streaming", "tv", "#C62828"),
		expense("software", "Logiciels", "app-window", "#D84315"),
		expense("sport", "Sport", "dumbbell", "#E53935")),
	expense("health", "Santé", "heart-pulse", "#AD1457"),
	expense("shopping", "Shopping", "shopping-bag", "#EF6C00"),
	expense("leisure", "Loisirs", "ticket", "#F9A825"),
	expense("taxes", "Impôts & taxes", "landmark", "#4E342E"),
	expense("bank_fees", "Frais bancaires", "banknote", "#5D4037"),
	expense("cash", "Retraits espèces", "wallet", "#757575"),
	{key: "income", name: "Revenus", kind: "income", icon: "trending-up", color: "#1B5E20", children: []seed{
		{key: "salary", name: "Salaire", kind: "income", icon: "briefcase", color: "#1B5E20"},
		{key: "refunds", name: "Remboursements", kind: "income", icon: "undo", color: "#33691E"},
		{key: "other_income", name: "Autres revenus", kind: "income", icon: "plus-circle", color: "#43A047"},
	}},
	{key: "transfers", name: "Virements internes", kind: "transfer", icon: "arrow-left-right", color: "#9E9E9E",
		excluded: true},
}
