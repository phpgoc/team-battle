# Team Battle Manager

[简体中文](README.md) · **English** · [한국어](README.ko.md)

Run tournaments between two teams. Round 1 is played as a series of 1v1 matches and round 2 uses the KOF format. If the teams win one round each, an ace battle decides the tournament. The app also provides a live scoreboard, tournament history, and individual player records.

## Manage Tournaments

- **Super administrators** manage accounts, create tournaments, enter past results, and maintain common tournament names, category icons, and backgrounds. They can manage any open tournament. Only account owners can change their login names. You cannot delete your own account or an account with match results.
- In **Tournament management**, super administrators can open any tournament by clicking its name. Use the controls on the right to delete or restore a closed tournament. Deleting a tournament keeps its data, hides it from the home page by default, and excludes its results from player records. Select **Show deleted tournaments** to see it. Restoring a tournament adds its results back to player records.
- A **tournament manager** is required when creating a tournament. Any active account, including a super administrator, can be selected. The manager handles team assignments, captains, lineups, scorekeeping, and final confirmation.
- Enter the tournament name, start time (to the hour), and manager. You can add players later. A tournament needs 6–12 players to start, and the teams can have different sizes. Assign players to Team A or B in advance or let the draw place them.
- Search for and add players one at a time, then select Team A, Team B, or random assignment on the right. You can remove players before the draw. Settings retain the entry order and assignments you entered. The actual teams appear in the live view and tournament history.
- **Save** and **Back to match** stay visible at the bottom of the settings page. **Save** applies the changes across the page. Before teams are formed, click **Start draw** above the ace battle settings. The draw shows team assignments, then the round 1 lineup and the KOF openers when those options are set to random. With an odd number of players, the last player joins Team A in the final draw animation. If captains set the round 1 lineup, they still need to submit it. Draw results survive a page refresh.
- **Before the team draw**, set the score targets and how players are selected for round 1 and KOF. The default targets are 4 and 5, respectively. The KOF opener can be drawn at random or chosen by the captain. **Before the first match starts**, set the ace battle probabilities for most wins, most losses, and special matchups. The remaining probability goes to a fully random draw. The player draw lasts 4 seconds by default and the ace draw lasts 5 seconds. Both durations can be changed during play. Super administrators can set defaults for future tournaments in **Site settings**.
- Click **Add special matchup** to add a group. Use the left and right multi-select fields to pick candidates for Teams A and B. Each group needs a probability greater than 0%. Most wins and most losses can be set to 0%. Before the team draw, the fields list players already assigned to each side. After the draw, they use the actual teams. If a side in a group has no candidates, the draw uses that full team. The fully random option includes all players. To draw only from the remaining players, add another special matchup and select them there.
- Click **Record win**, check the players and the winner in the dialog, and click **Confirm**. Confirmation requires a click, and Esc cancels the dialog. The first result starts the tournament. A scheduled matchup moves straight into the current match area. If players still need to be drawn, the draw animation plays. When KOF ends and each team has won a round, check the scores and click **Draw ace players**.
- To fix a wrong result, open **Tournament settings** in the top right and find **Correct result**. The page lists all completed matches. Select the correct winner and save. You can correct one result at a time. Later matches are cleared and play resumes from the corrected result. For example, correcting a round 1 score from 3–2 to 4–1 removes the extra sixth match and advances to KOF.
- After the ace draw, you can change either ace player in **Tournament settings** and save. When all matches are complete, the tournament is **Awaiting final confirmation**. The manager can correct results until they select **Confirm tournament end** in settings. Confirmation locks the results. Administrators can switch the advertising or sponsor background in tournament settings, and all viewers see the change.
- Super administrators can upload, rename, and delete images in **Tournament backgrounds**. Backgrounds assigned to a tournament cannot be deleted. Uploads generate compressed thumbnails for the management page. The tournament view uses the original image.
- When entering a past tournament, set the teams and score targets first. With an even number of players, the teams must be the same size. With an odd number, Team A must have one more player. Enter round 1 and KOF separately. Clicking the winner records the result immediately and updates the score. In KOF, the winner stays on and you choose the losing team's next player. For a revival, select the player who actually returned. Enter an ace battle if the teams won one round each. You can re-enter results starting at any match, which clears that match and all later results. Check the results before confirming the final save, which locks the tournament.

## Player Actions

- If your team has no captain, click the action area on the right of another teammate's name and select **Vote for captain**. A player becomes captain with votes from half the team, rounded up. The manager can appoint or remove a captain in the same area. Once a captain is in place, other teammates use the spectator view.
- When captains set the round 1 lineup, open **Round 1 lineup** from the tournament page and save the playing order. The entry is available in this mode. Once one side submits, its public status reads **Lineup submitted**. The matchups appear after both sides submit. The manager can switch between Teams A and B on the lineup page and submit for either side.
- In KOF, use the action area on the right of a player's row. Captains can pick their teammates. If your team has no captain, click your own row to sign up. Managers can pick any eligible player on either side. You can queue a player in advance, and only the queued player sees that status. When a match ends, the losing team's queued player fills the next slot. Play waits if the slot is empty. Players who have lost cannot sign up. If the score target exceeds the team size and everyone on the team has lost once, a player is drawn at random to return. If either team may need at least two such revivals, you can enable the no-repeat KOF revival rule before the team draw. With this option, everyone revives once before another cycle begins. It is off by default. Click a player's name to view their record.
- In account settings, you can change your login name, password, display name, avatar, and category. For regular accounts, all non-password edits share a seven-day cooldown. Those edits are also locked while you are entered in an open tournament. Super administrators can update profiles at any time. Only account owners can change their login names. You can change your password at any time by entering just the new password twice. You stay signed in until you sign out or sign in on another device.

## Spectating and History

Anyone can watch without signing in. On the home page, browse past tournaments by page, combine filters for player, tournament name, and start date, or open a live tournament. Spectating opens as soon as six players have been added. Before the team draw, they appear in a single roster. Only the tournament manager and super administrators can see tournaments with fewer players. The live scoreboard updates automatically. Live and past tournaments use the same full-screen view. The **Tournament rules** section at the bottom covers all three rounds, including the ace battle when it wasn't played. For tournaments recorded later, this section shows just **Entered retrospectively**. Managers can edit settings from the top right. Click a player's name to see team tournament wins and losses, individual wins and losses overall and by opponent category, and ace battle results. Team results count confirmed tournaments, including players on the official roster who did not play a match. Your own record is also available in account settings. Switching between Chinese, English, and Korean keeps you on the current page.

On the roster and in the current matchup, players turn red after their first win in the tournament. Players with no wins appear green. These colors carry over between rounds. In live KOF, players who have lost are struck through. History keeps the colors and shows all names without strikethrough. Use the three-line menu at the top right to adjust panel transparency (30% by default, saved in your browser), switch languages, go home, or sign in. The lineup and tournament settings controls appear beside this menu for managers.

## Build and Deploy

Install Go, then build from the project directory.

```sh
go build -o team-battle .
./team-battle
```

For Windows PowerShell, use the executable name below.

```powershell
go build -o team-battle.exe .
.\team-battle.exe
```

### Cross-compile for Linux

From Windows PowerShell, build for Linux x86-64.

```powershell
$env:CGO_ENABLED = '0'
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
go build -trimpath -o team-battle-linux-amd64 .
Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH
```

From Linux or macOS, set the build variables on the command line.

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o team-battle-linux-amd64 .
```

For an ARM64 server, use `arm64` in place of `amd64`. Copy the executable to Linux and run it.

```sh
chmod +x team-battle-linux-amd64
./team-battle-linux-amd64 --host 127.0.0.1
```

The default address is `http://localhost:9527`. On first launch, the app creates its data directory and a super administrator account named `admin` with password `test1234`. Sign in and change the password. Deployment requires one executable. Keep the data files after launch. Avatars and category icons are stored in the database. Backgrounds are stored in `uploads/backgrounds/` alongside it.

To upgrade, stop the app and back up the database and the adjacent `uploads/` directory. Replace the executable and start it. The app upgrades the database automatically and preserves existing data. It stops if the upgrade fails. To return to an earlier version, also restore the pre-upgrade backup.

| Option | Default | Purpose |
| --- | --- | --- |
| `--db` | `./runtime/team-battle.sqlite` | Database path |
| `--host` | `localhost` | Listen address |
| `--port` | `9527` | Listen port |
| `--sse-max-connections` | `500` | Shared limit on live update connections across all tournaments. Browsers retry automatically when the limit is reached. |

### Nginx Reverse Proxy

The app serves HTTP. Configure the HTTPS certificate in Nginx. Run the app on `127.0.0.1:9527` and add this configuration inside your site's Nginx `server` block.

```nginx
client_max_body_size 50m;

location / {
    proxy_pass http://127.0.0.1:9527;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
}
```

Keep proxy buffering off so viewers receive live scores promptly. Check the configuration with `nginx -t`, then apply it with `nginx -s reload`.

### Demo Data

The repository's `test.sqlite` contains tournaments at different stages and sample images. The administrator login name is `admin`. Players sign in as `Flash`, `Jaedong`, `Bisu`, `Stork`, `sAviOr`, `iloveoov`, `NaDa`, `Rain`, or `soO`, matching their display names. All demo accounts use the password `test1234`.

```sh
go run . --db test.sqlite
```

The demo database and images are separate from the executable. Create a fresh database for production use.
