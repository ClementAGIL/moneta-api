// Generates testdata/demo-*.csv: twelve months of synthetic transactions for a current account and a savings account
// (subscriptions with a price increase, rent, salary, groceries, a bakery unknown to the built-in rules, a monthly
// transfer to savings). Deterministic.
// Run: node scripts/gen-testdata.mjs
import { writeFileSync } from 'node:fs';

const MONTHS = ['2025-11', '2025-12', '2026-01', '2026-02', '2026-03', '2026-04', '2026-05', '2026-06', '2026-07',
  '2026-08', '2026-09', '2026-10'];
let seed = 42;
const rand = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
const euros = (cents) => (cents / 100).toFixed(2).replace('.', ',');
const day = (month, d) => `${month}-${String(d).padStart(2, '0')}`;

const current = [];
const savings = [];
MONTHS.forEach((month, i) => {
  current.push([day(month, 1), 'PRLV SEPA LOYER SCI DES LILAS', -85000]);
  current.push([day(month, 2), 'PRLV SEPA NETFLIX.COM', i < 10 ? -1349 : -1599]);
  current.push([day(month, 5), 'PRLV SEPA SPOTIFY AB', -1199]);
  current.push([day(month, 7), 'PRLV SEPA FREE MOBILE', -1999]);
  current.push([day(month, 10), 'PRLV SEPA EDF CLIENTS PARTICULIERS', -8900]);
  current.push([day(month, 12), 'CARTE BASIC FIT CB*1234', -2999]);
  current.push([day(month, 27), 'VIR SEPA RECU /DE ACME SAS /MOTIF SALAIRE', 245000]);
  current.push([day(month, 28), 'VIR SEPA EMIS VERS LIVRET A', -30000]);
  savings.push([day(month, 28), 'VIR SEPA RECU DE COMPTE COURANT', 30000]);
  for (let w = 0; w < 4; w++) {
    current.push([day(month, 3 + w * 7), 'CARTE CARREFOUR MARKET CB*1234', -Math.round(4000 + rand() * 6000)]);
    current.push([day(month, 4 + w * 7), 'CARTE RESTAURANT LE PETIT ZINC CB*1234', -Math.round(1500 + rand() * 3500)]);
    // A merchant no built-in rule knows: the classifier learns it from the user's corrections.
    current.push([day(month, 5 + w * 7), 'CARTE LE FOURNIL DE MARIE CB*1234', -Math.round(300 + rand() * 900)]);
  }
  if (i % 2 === 0) current.push([day(month, 18), 'CARTE AMAZON EU SARL CB*1234', -Math.round(2000 + rand() * 8000)]);
});

const csv = (rows) =>
  'Date;Libellé;Montant\n' + rows.map(([d, l, a]) => `${d};${l};${euros(a)}`).join('\n') + '\n';
writeFileSync(new URL('../testdata/demo-current.csv', import.meta.url), csv(current));
writeFileSync(new URL('../testdata/demo-savings.csv', import.meta.url), csv(savings));
console.log(`${current.length} + ${savings.length} rows written`);
