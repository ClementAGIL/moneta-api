// Built-in rules: common French merchants and label keywords → default category keys. Matched as whole words on the
// normalized label, after the user's own rules. Extend freely; keep one keyword per line group for readability.
package categorize

type builtin struct {
	keyword string
	key     string // default category key (see categories/defaults.go)
	sign    string // "out", "in" or "any"
}

func out(key string, keywords ...string) []builtin { return group(key, "out", keywords) }
func in(key string, keywords ...string) []builtin  { return group(key, "in", keywords) }

func group(key, sign string, keywords []string) []builtin {
	rules := make([]builtin, len(keywords))
	for i, k := range keywords {
		rules[i] = builtin{keyword: k, key: key, sign: sign}
	}
	return rules
}

var builtins = concat(
	out("groceries", "CARREFOUR", "LECLERC", "AUCHAN", "INTERMARCHE", "LIDL", "ALDI", "MONOPRIX", "FRANPRIX", "CASINO",
		"SUPER U", "HYPER U", "U EXPRESS", "SYSTEME U", "COOP", "BIOCOOP", "NATURALIA", "PICARD", "GRAND FRAIS",
		"NETTO", "LEADER PRICE", "SPAR", "CORA", "MATCH", "PROXI", "VIVAL", "BOULANGERIE", "BOUCHERIE", "PRIMEUR"),
	out("restaurants", "RESTAURANT", "RESTO", "MCDONALD", "MC DONALD", "BURGER KING", "KFC", "QUICK", "SUBWAY",
		"DOMINOS", "PIZZA", "STARBUCKS", "PAUL", "BRIOCHE DOREE", "DELIVEROO", "UBER EATS", "UBEREATS", "JUST EAT",
		"BRASSERIE", "CAFE", "BAR", "SUSHI", "KEBAB"),
	out("rent", "LOYER", "FONCIA", "NEXITY", "CITYA"),
	out("energy", "EDF", "ENGIE", "TOTALENERGIES", "TOTAL ENERGIES", "GDF", "ENI", "EKWATEUR", "VEOLIA", "SUEZ",
		"EAU DE"),
	out("telecom", "ORANGE", "SFR", "BOUYGUES", "FREE MOBILE", "FREE TELECOM", "FREE", "SOSH", "RED BY SFR",
		"B AND YOU", "LA POSTE MOBILE"),
	out("home_insurance", "MAIF", "MACIF", "MATMUT", "GMF", "AXA", "ALLIANZ", "GROUPAMA", "MMA", "LUKO"),
	out("fuel", "TOTAL", "ESSO", "SHELL", "BP", "AVIA", "STATION", "CARBURANT"),
	out("public_transport", "RATP", "NAVIGO", "TCL", "TISSEO", "TAN", "STAR", "TRANSDEV", "KEOLIS", "UBER", "BOLT",
		"LIME", "VELIB", "BLABLACAR"),
	out("travel", "SNCF", "OUIGO", "TRAINLINE", "AIR FRANCE", "EASYJET", "RYANAIR", "TRANSAVIA", "VUELING",
		"BOOKING", "AIRBNB", "HOTEL", "FLIXBUS"),
	out("parking", "PEAGE", "VINCI", "SANEF", "APRR", "ASF", "INDIGO", "PARKING", "ULYS", "BIP GO"),
	out("streaming", "NETFLIX", "SPOTIFY", "DEEZER", "DISNEY", "CANAL", "PRIME VIDEO", "AMAZON PRIME", "APPLE TV",
		"YOUTUBE", "MOLOTOV", "OCS", "PARAMOUNT", "CRUNCHYROLL", "MAX"),
	out("software", "GOOGLE", "APPLE COM", "ICLOUD", "MICROSOFT", "ADOBE", "DROPBOX", "OPENAI", "ANTHROPIC",
		"GITHUB", "NOTION", "OVH"),
	out("sport", "BASIC FIT", "BASIC-FIT", "FITNESS PARK", "NEONESS", "KEEP COOL", "L ORANGE BLEUE", "DECATHLON",
		"GO SPORT", "SALLE DE SPORT"),
	out("health", "PHARMACIE", "PHARMA", "DOCTEUR", "DR", "MEDECIN", "CABINET", "DENTISTE", "OPTICIEN", "LABORATOIRE",
		"KINE", "HOPITAL", "CLINIQUE", "DOCTOLIB", "MUTUELLE", "HARMONIE", "MGEN", "ALAN"),
	out("shopping", "AMAZON", "AMZN", "FNAC", "DARTY", "BOULANGER", "CDISCOUNT", "IKEA", "LEROY MERLIN",
		"CASTORAMA", "ZARA", "H&M", "UNIQLO", "KIABI", "PRIMARK", "SEPHORA", "ACTION", "GIFI", "LA HALLE", "VINTED",
		"ALIEXPRESS", "SHEIN", "TEMU", "ZALANDO", "APPLE STORE", "BOULANGER"),
	out("leisure", "CINEMA", "UGC", "PATHE", "GAUMONT", "MK2", "FNAC SPECTACLES", "TICKETMASTER", "STEAM",
		"PLAYSTATION", "NINTENDO", "XBOX", "MUSEE", "CULTURA"),
	out("taxes", "DGFIP", "IMPOT", "IMPOTS", "TRESOR PUBLIC", "URSSAF", "AMENDE", "ANTAI", "TAXE"),
	out("bank_fees", "FRAIS", "COTISATION", "COMMISSION", "AGIOS", "INTERETS DEBITEURS", "TENUE DE COMPTE"),
	out("cash", "RETRAIT", "DAB", "GAB"),
	in("salary", "SALAIRE", "PAIE", "REMUNERATION", "TRAITEMENT"),
	in("refunds", "REMBOURSEMENT", "REMB", "CPAM", "AMELI", "MUTUELLE", "AVOIR", "REFUND"),
	in("other_income", "CAF", "POLE EMPLOI", "FRANCE TRAVAIL", "INTERETS", "DIVIDENDE"),
)

func concat(groups ...[]builtin) []builtin {
	var all []builtin
	for _, g := range groups {
		all = append(all, g...)
	}
	return all
}
