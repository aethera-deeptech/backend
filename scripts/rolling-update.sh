#!/usr/bin/env bash
# Updates the API copies of a host one at a time, so the other copy keeps answering while one restarts.
#
# usage  ./rolling-update.sh <version>        for example ./rolling-update.sh 0.1.0
#
# What it needs on the host. Docker with the compose plugin (v2), curl, jq and flock. A compose file with one service for each copy,
# api-1 and api-2 in compose.production.example.yml, listening on the loopback ports API_1_HOST_PORT and API_2_HOST_PORT of the env
# file. A reverse proxy that stops sending requests to a copy that is down, because that is what keeps the site up while a copy
# restarts. Run it in tmux or screen, so a dropped ssh connection does not stop it half way.
#
# What it does, in this order. It checks that the services exist, then pulls the image of the version, which also proves that the
# tag exists, and only then sets IMAGE_TAG in the env file. When the pull fails it changes nothing, and an interrupt before the
# first copy puts the env file back as it was. Then for each copy in order it recreates that one copy with no dependencies and
# polls /healthz of that copy until the answer is status ok and the version that was asked for, twice in a row. It goes on to the
# next copy only when the one before is healthy. When a copy does not become healthy it stops, says which copies are already new
# and which are still old, and prints the command that goes back. Nothing is rolled back by itself.
#
# Going back to an older version is allowed only when ROLLBACK_OK is set to that version, because a newer version may have applied
# a database migration that the older version cannot run on. The service has no migrations yet. Read the changelog first once it has.
#
# Settings, all optional, given as environment variables
#   COMPOSE_DIR   the folder with the compose file and the env file, default the folder of this script
#   COMPOSE_FILE  the compose file, default compose.yml, as docker compose reads it
#   COPIES        the services and the loopback port of each, in update order, default api-1 and api-2 on API_1_HOST_PORT and
#                 API_2_HOST_PORT of the env file, which are 8010 and 8011 when unset
#   TIMEOUT       seconds to wait for one copy to become healthy, default 120
#   ROLLBACK_OK   the version that you confirm to go back to
#   DRY_RUN       1, true or yes prints the steps and changes nothing, but it still checks the services
set -euo pipefail

die() {
  echo "$*" >&2
  exit 1
}

version="${1:?usage rolling-update.sh <version>, for example 0.1.0}"
version="${version#v}"
script="$(realpath "${BASH_SOURCE[0]}")"
compose_dir="${COMPOSE_DIR:-$(dirname "$script")}"
# the services of the compose file load .env too, so the file name is not a setting, or the two could name different files
env_file=".env"
timeout="${TIMEOUT:-120}"

case "${DRY_RUN:-0}" in
  1 | true | yes) dry_run=1 ;;
  0 | false | no | "") dry_run=0 ;;
  *) die "DRY_RUN must be 1, true, yes, 0, false or no, got ${DRY_RUN}" ;;
esac

for tool in docker curl jq flock; do
  command -v "$tool" >/dev/null || die "$tool is not installed"
done

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "The version must look like 1.2.3, got $version"
[[ "$timeout" =~ ^[0-9]+$ ]] || die "TIMEOUT must be a number of seconds, got $timeout"

cd "$compose_dir"
[ -f "$env_file" ] || die "$env_file was not found in $compose_dir"

# env_value prints the last value of a setting in the env file, without quotes or a carriage return
env_value() {
  sed -n "s/^$1=//p" "$env_file" | tail -1 | tr -d "\r\"'"
}

# without COPIES the ports of the copies are the host ports of the env file, so a change of API_1_HOST_PORT or API_2_HOST_PORT there
# is followed here, and the script cannot poll another port than the one the copy listens on
if [ -n "${COPIES:-}" ]; then
  copies="$COPIES"
else
  port_1="$(env_value API_1_HOST_PORT)"
  port_2="$(env_value API_2_HOST_PORT)"
  copies="api-1:${port_1:-8010} api-2:${port_2:-8011}"
fi

services=()
ports=()
for copy in $copies; do
  [[ "$copy" =~ ^([A-Za-z0-9_.-]+):([0-9]+)$ ]] || die "Every entry of COPIES must look like service:port, got $copy"
  [[ " ${services[*]:-} " != *" ${BASH_REMATCH[1]} "* ]] || die "The service ${BASH_REMATCH[1]} is listed twice in COPIES"
  [[ " ${ports[*]:-} " != *" ${BASH_REMATCH[2]} "* ]] || die "The port ${BASH_REMATCH[2]} is listed twice in COPIES"
  services+=("${BASH_REMATCH[1]}")
  ports+=("${BASH_REMATCH[2]}")
done
[ "${#services[@]}" -gt 0 ] || die "COPIES lists no service"

# only one update runs at a time, because two of them would fight over the env file and the same copy
exec 9>.rolling-update.lock
flock -n 9 || die "Another update is running on this host"

# IMAGE_TAG is set for every compose command, because a value that is exported in the shell of the operator would otherwise win
# over the env file and the copies would start on another tag than the one that was pulled and checked. The two host ports are
# cleared for the same reason, because the polling reads them from the env file and must see the ports that compose publishes
compose() {
  env -u API_1_HOST_PORT -u API_2_HOST_PORT IMAGE_TAG="$version" docker compose --env-file "$env_file" "$@"
}

run() {
  if [ "$dry_run" = "1" ]; then
    echo "DRY RUN $*"
  else
    "$@"
  fi
}

# healthy succeeds only when /healthz of one copy answers status ok and the version that was asked for
healthy() {
  local port="$1" body
  body="$(curl -sS --max-time 5 "http://127.0.0.1:${port}/healthz" 2>/dev/null)" || return 1
  echo "$body" | jq -c . 2>/dev/null || return 1
  echo "$body" | jq -e --arg v "$version" '.status == "ok" and .version == $v' >/dev/null
}

# the tag of the env file as it is now, and the whole file byte for byte. The x keeps the newlines at the end, and it is cut off again
previous="$(env_value IMAGE_TAG)"
env_original="$(
  cat "$env_file"
  printf x
)"
env_original="${env_original%x}"
changed=0
updated=()
# 1 from the moment the first copy is recreated, because from then on a copy may run the new version
copy_started=0

# restore_tag puts the env file back as it was, for a step that fails before any copy is touched. Without it the env file would name
# a version that no copy runs, and a later docker compose up -d would recreate every copy on it at once
restore_tag() {
  [ "$changed" = "1" ] || return 0

  if printf '%s' "$env_original" >"$env_file"; then
    changed=0
    return 0
  fi

  echo "IMAGE_TAG in $env_file could not be put back, set it to ${previous:-not set} by hand before anything runs docker compose up -d" >&2
  return 1
}

on_signal() {
  echo >&2
  if [ "$copy_started" = "0" ]; then
    restore_tag || true
  fi
  echo "Interrupted. IMAGE_TAG in $env_file is $([ "$changed" = "1" ] && echo "$version" || echo "${previous:-not set}"), copies updated so far ${updated[*]:-none}." >&2
  echo "Check the copies with docker compose ps and run the update again when they are fine." >&2
  exit 130
}
trap on_signal INT TERM HUP

echo "Checking that the services exist"
known="$(compose config --services)"
for service in "${services[@]}"; do
  grep -qxF -- "$service" <<<"$known" || die "The service $service is not in the compose file, nothing was changed. The services are $(echo "$known" | tr '\n' ' ')"
done

# going back needs a confirmation, because a newer version may have changed the database in a way that the older one cannot run on
if [ -n "$previous" ] && [[ "$previous" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] && [ "$previous" != "$version" ] &&
  [ "$(printf '%s\n%s\n' "$previous" "$version" | sort -V | head -1)" = "$version" ]; then
  echo "WARNING $version is older than $previous, which runs now. A newer version may have applied a database migration that $version cannot run on." >&2
  rollback_ok="${ROLLBACK_OK:-}"
  [ "${rollback_ok#v}" = "$version" ] || die "Going back needs ROLLBACK_OK=$version, nothing was changed"
fi

# compose pulls the image that the services of the compose file really use, so a compose file that names another image than IMAGE
# is caught here, before anything is changed. IMAGE_TAG is the version for this command, so the env file is not touched yet
echo "Pulling the image of the version for ${services[*]}, which also proves that the tag exists"
if [ "$dry_run" = "1" ]; then
  echo "DRY RUN docker compose pull ${services[*]}"
else
  compose pull "${services[@]}" || die "The image of $version could not be pulled, so nothing was changed. The tag may not exist or Docker Hub refused the pull"
fi

echo "Setting IMAGE_TAG to $version in $env_file"
if [ "$dry_run" = "1" ]; then
  echo "DRY RUN set IMAGE_TAG=$version in $env_file"
else
  if grep -q '^IMAGE_TAG=' "$env_file"; then
    sed -i --follow-symlinks "s/^IMAGE_TAG=.*/IMAGE_TAG=${version}/" "$env_file"
  else
    printf '\nIMAGE_TAG=%s\n' "$version" >>"$env_file"
  fi
  changed=1
fi

# the command that goes back, with the folder and the env file when they are not the defaults
go_back() {
  local target="${previous:-OLD}" prefix=""
  [ -z "${COMPOSE_DIR:-}" ] || prefix="${prefix}COMPOSE_DIR=${COMPOSE_DIR} "
  echo "${prefix}ROLLBACK_OK=${target} ${script} ${target}"
}

# half_done tells the operator what runs where after a copy failed, and why a plain docker compose up -d is a mistake now
half_done() {
  local failed="$1" index="$2" rest=() later
  for later in "${services[@]:$((index + 1))}"; do
    rest+=("$later")
  done
  echo "$failed was recreated on $version and is not healthy." >&2
  echo "Already healthy on $version ${updated[*]:-none}. Still on ${previous:-the old version} ${rest[*]:-none}." >&2
  echo "IMAGE_TAG in $env_file names $version now, so do not run docker compose up -d before the copies are checked, because it would recreate every copy on $version." >&2
  echo "Nothing was rolled back. To go back run $(go_back), and read the logs with docker compose logs $failed first." >&2
}

for i in "${!services[@]}"; do
  service="${services[$i]}"
  port="${ports[$i]}"

  echo "Updating $service on port $port"
  copy_started=1
  if ! run compose up -d --no-deps --force-recreate "$service"; then
    echo "$service could not be recreated. Check docker compose ps and the logs of $service." >&2
    half_done "$service" "$i"
    exit 1
  fi

  if [ "$dry_run" = "1" ]; then
    updated+=("$service")
    continue
  fi

  ok=0
  answer=""
  start="$SECONDS"
  while [ $((SECONDS - start)) -lt "$timeout" ]; do
    if answer="$(healthy "$port")"; then
      ok=$((ok + 1))
      echo "$service healthy ($ok of 2) $answer"
      if [ "$ok" -ge 2 ]; then
        break
      fi
    else
      ok=0
      echo "$service not healthy yet after $((SECONDS - start))s, the last answer was ${answer:-none}"
    fi
    sleep 2
  done

  if [ "$ok" -lt 2 ]; then
    echo "$service did not become healthy within ${timeout} seconds." >&2
    half_done "$service" "$i"
    exit 1
  fi
  updated+=("$service")
done

echo "Done. ${updated[*]} run $version."
