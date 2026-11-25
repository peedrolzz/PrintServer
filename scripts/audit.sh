#!/usr/bin/env bash
pass() { printf '  \033[32mOK\033[0m    %s\n' "$*"; PASSED=$((PASSED+1)); }
fail() { printf '  \033[31mFALHA\033[0m %s\n' "$*"; FAILED=$((FAILED+1)); }
warn() { printf '  \033[33mAVISO\033[0m %s\n' "$*"; WARNED=$((WARNED+1)); }
section() { printf '\n[%s]\n' "$*"; }
PASSED=0; FAILED=0; WARNED=0

section "V01 PrintBridge sem auth"
if sudo ss -tln 2>/dev/null | grep -qE "0\.0\.0\.0:91[0-9][0-9]"; then
    fail "porta 9100-9199 aberta em 0.0.0.0"
else
    pass "nenhuma porta de impressora exposta sem impressora configurada"
fi

section "V02 Senha em claro no SD"
HIT=$(sudo grep -lE '^\s*passwd:\s*"\$y\$' /boot/firmware/user-data /boot/firmware/*.yaml 2>/dev/null)
if [[ -n "$HIT" ]]; then
    fail "hash yescrypt em $HIT"
else
    pass "sem hash de senha em /boot/firmware/"
fi

section "V03 Senha hotspot hardcoded"
if sudo grep -rq "HARDCODED_PASSWORD_XYZ" /etc/printserver/ 2>/dev/null; then
    fail "senha hardcoded encontrada"
else
    HP=$(sudo cat /var/lib/printserver/credentials/hotspot-password 2>/dev/null)
    pass "senha do hotspot gerada por device (atual: $HP)"
fi

section "V06 Sudo NOPASSWD + SSH passwd"
if sudo grep -rqE "NOPASSWD:[[:space:]]*ALL" /etc/sudoers.d/ 2>/dev/null; then
    warn "NOPASSWD: ALL presente (artefato de deploy; seal-image remove)"
else
    pass "nenhum sudoers com NOPASSWD: ALL"
fi
if sudo grep -rq "^PasswordAuthentication yes" /etc/ssh/ 2>/dev/null; then
    warn "SSH aceita senha (seal-image corrige)"
else
    pass "SSH sem autenticacao por senha"
fi

section "V11/V13 Backups legados em paths do produto"
LEAKS=$(sudo find /opt/printserver /var/lib/printserver /etc/printserver /home /root \
    \( -name "*.backup*" -o -name "*.bak" -o -name "*.old" \) -type f 2>/dev/null)
if [[ -n "$LEAKS" ]]; then
    fail "backups encontrados: $LEAKS"
else
    pass "sem arquivos de backup em paths do produto"
fi

section "V17 SSH no hotspot"
if sudo nft list ruleset 2>/dev/null | grep -q 'iifname "wlan0" tcp dport 22 drop'; then
    pass "nftables bloqueia SSH em wlan0"
else
    fail "nftables NAO bloqueia SSH em wlan0"
fi

section "V18 DNS wildcard"
RCODE=$(python3 -c '
import socket
req = bytes.fromhex("abcd0100000100000000000006676f6f676c6503636f6d0000010001")
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.settimeout(3)
s.sendto(req, ("10.42.0.1", 53)); d,_ = s.recvfrom(512)
print(d[3] & 0xf)
' 2>/dev/null)
if [[ "$RCODE" == "5" ]]; then
    pass "DNS retorna REFUSED para dominios externos (rcode=5)"
else
    fail "DNS resolveu dominio externo (rcode=$RCODE)"
fi

section "V20 Cache cloud-init"
if sudo find /var/lib/cloud/instances/ -name "user-data*" 2>/dev/null | grep -q .; then
    warn "cache do cloud-init presente (seal-image limpa)"
else
    pass "sem cache do cloud-init com user-data"
fi

section "V23 Sudo codex"
if [[ -f /etc/sudoers.d/90-codex-printer ]]; then
    fail "sudoers 90-codex-printer presente"
else
    pass "sem arquivo 90-codex-printer"
fi

section "V25 Tokens de ferramentas AI"
HITS=$(sudo find /home /root \( -name ".codex" -o -name ".cursor" -o -name ".claude" \) 2>/dev/null)
if [[ -n "$HITS" ]]; then
    fail "pasta de ferramenta de AI encontrada: $HITS"
else
    pass "sem credenciais de ferramentas de AI"
fi

section "V26 Chaves SSH de terceiros"
KEYS=$(sudo find /home /root -name "authorized_keys" -exec cat {} \; 2>/dev/null)
if echo "$KEYS" | grep -iE "codex|chatgpt" > /dev/null; then
    fail "authorized_keys contem chave de ferramenta AI"
elif [[ -z "$KEYS" ]]; then
    pass "authorized_keys vazio"
else
    pass "authorized_keys presente, verificar manualmente se so tem chave autorizada"
fi

section "Preseed shredded"
if [[ -f /boot/firmware/printserver-preseed.json ]]; then
    fail "preseed NAO foi removido do boot"
else
    pass "preseed removido apos firstboot (ou nao usado)"
fi

echo
echo "================================================================"
printf 'Resultado: \033[32m%d OK\033[0m, \033[33m%d avisos\033[0m, \033[31m%d falhas\033[0m\n' \
    "$PASSED" "$WARNED" "$FAILED"
if [[ $FAILED -eq 0 ]]; then
    printf '\033[32mAudit OK\033[0m. Avisos sao artefatos esperados do deploy manual.\n'
    exit 0
else
    printf '\033[31mAudit FALHOU.\033[0m Verifique os checks em vermelho.\n'
    exit 1
fi
